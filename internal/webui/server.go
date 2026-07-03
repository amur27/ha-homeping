// Пакет webui — локальный веб-интерфейс настроек агента.
// Логика модуля: HTTP-сервер строго на 127.0.0.1 с эфемерным портом;
// доступ защищён сессионным токеном (crypto/rand): браузер открывается
// по ссылке с ?auth=…, токен переносится в cookie (SameSite=Strict),
// запросы без него получают 403 (docs/spec.md, раздел 8.1). API (раздел 8.3):
// статус, чтение/запись конфига с hot-reload, сохранение токена HA
// в системное хранилище, тестовое уведомление. Токен HA и сессионный токен
// никогда не логируются и не возвращаются клиенту.
package webui

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"homeping/internal/agent"
	"homeping/internal/config"
	"homeping/internal/secrets"
)

// Страница настроек: один HTML-файл без внешних зависимостей и CDN.
//
//go:embed assets/index.html
var assets embed.FS

// cookieName — имя cookie с сессионным токеном.
const cookieName = "homeping_auth"

// Server — веб-интерфейс настроек одного работающего агента.
type Server struct {
	// Agent — супервизор: статус, reload, тестовое уведомление.
	Agent *agent.Agent
	// Version показывается в статусе и подвале страницы.
	Version string
	// ConfigPath — файл конфигурации, который редактирует интерфейс.
	ConfigPath string

	authToken string
	listener  net.Listener
	srv       *http.Server
}

// Start начинает слушать 127.0.0.1 на эфемерном порту и обслуживать
// запросы в фоне. Возвращает ошибку, если порт занять не удалось.
func (s *Server) Start() error {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return fmt.Errorf("не удалось создать сессионный токен: %w", err)
	}
	s.authToken = hex.EncodeToString(token)

	// Только loopback: интерфейс недоступен из локальной сети.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("не удалось открыть порт настроек: %w", err)
	}
	s.listener = ln
	s.srv = &http.Server{Handler: s.routes()}

	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("веб-интерфейс настроек остановился с ошибкой", "error", err)
		}
	}()
	slog.Info("веб-интерфейс настроек запущен", "addr", ln.Addr().String())
	return nil
}

// URL возвращает адрес страницы настроек со свежим сессионным токеном —
// его открывает браузер по клику в меню трея. Ссылка не логируется.
func (s *Server) URL() string {
	return fmt.Sprintf("http://%s/?auth=%s", s.listener.Addr().String(), s.authToken)
}

// Close останавливает сервер.
func (s *Server) Close() error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Close()
}

// routes собирает обработчики; выделено для httptest в юнит-тестах.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handlePutConfig)
	mux.HandleFunc("POST /api/token", s.handleToken)
	mux.HandleFunc("POST /api/test", s.handleTest)
	return s.auth(mux)
}

// auth — middleware сессионного токена: принимает токен из cookie либо
// из query-параметра auth (первый заход по ссылке из трея — тогда токен
// переносится в cookie). Всё остальное — 403.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(cookieName); err == nil && s.tokenOK(c.Value) {
			next.ServeHTTP(w, r)
			return
		}
		if q := r.URL.Query().Get("auth"); q != "" && s.tokenOK(q) {
			http.SetCookie(w, &http.Cookie{
				Name:     cookieName,
				Value:    q,
				Path:     "/",
				HttpOnly: true,
				// Strict: cookie не уходит с межсайтовыми запросами —
				// защита от CSRF со сторонних страниц.
				SameSite: http.SameSiteStrictMode,
			})
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "доступ запрещён: откройте настройки через меню HomePing в трее", http.StatusForbidden)
	})
}

// tokenOK сравнивает предъявленный токен с сессионным за постоянное время.
func (s *Server) tokenOK(candidate string) bool {
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(s.authToken)) == 1
}

// handleIndex отдаёт страницу настроек.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := assets.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, "страница настроек не встроена в сборку", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// statusResponse — тело GET /api/status.
type statusResponse struct {
	State    string `json:"state"`
	Paused   bool   `json:"paused"`
	Detail   string `json:"detail,omitempty"`
	Version  string `json:"version"`
	TokenSet bool   `json:"token_set"`
}

// handleStatus отдаёт состояние агента; сам токен не возвращается никогда,
// только признак «задан/не задан» — и только из системного хранилища
// (токен из переменной окружения интерфейс не редактирует).
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	st := s.Agent.Status()
	_, tokenErr := secrets.Get()
	writeJSON(w, http.StatusOK, statusResponse{
		State:    st.State.String(),
		Paused:   st.Paused,
		Detail:   st.Detail,
		Version:  s.Version,
		TokenSet: tokenErr == nil,
	})
}

// handleGetConfig отдаёт текущий конфиг с диска (JSON-представление YAML).
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load(s.ConfigPath)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "конфигурация на диске невалидна: " + err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// handlePutConfig валидирует и атомарно сохраняет конфиг, затем hot-reload.
// Ошибки валидации — 422 с описанием на русском (docs/spec.md, раздел 8.3).
func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var cfg config.Config
	dec := json.NewDecoder(r.Body)
	// Неизвестные поля — ошибка, зеркально strict-режиму YAML.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error": "не удалось разобрать данные формы: " + err.Error(),
		})
		return
	}
	if err := config.Save(&cfg, s.ConfigPath); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if err := s.Agent.Reload(); err != nil {
		// Save только что записал валидный конфиг; сюда можно попасть
		// лишь при гонке с ручной правкой файла.
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("конфигурация сохранена из веб-интерфейса", "entities", len(cfg.Entities))
	writeJSON(w, http.StatusOK, map[string]string{"result": "сохранено и применено"})
}

// handleToken сохраняет токен HA в системное хранилище и перезапускает
// сессию агента. Токен не логируется и не возвращается.
func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Token) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error": "токен пуст — вставьте long-lived токен из профиля Home Assistant",
		})
		return
	}
	if err := secrets.Set(strings.TrimSpace(body.Token)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	slog.Info("токен home assistant сохранён в системное хранилище")
	if err := s.Agent.Reload(); err != nil {
		// Токен сохранён, но конфиг на диске невалиден — сообщаем об этом.
		writeJSON(w, http.StatusOK, map[string]string{
			"result": "токен сохранён; конфигурация не применена: " + err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"result": "токен сохранён"})
}

// handleTest показывает пробное уведомление.
func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	if err := s.Agent.TestNotification(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "не удалось показать уведомление: " + err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"result": "уведомление показано"})
}

// writeJSON сериализует ответ с корректным Content-Type.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("не удалось сериализовать ответ веб-интерфейса", "error", err)
	}
}
