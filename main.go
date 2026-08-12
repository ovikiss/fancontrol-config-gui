package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

var settingsMu sync.Mutex

const settingsFile = "settings.json"

type healthResponse struct {
	OK      bool   `json:"ok"`
	Service string `json:"service"`
}

func main() {
	port := envOr("PORT", "4173")
	sharedDir := sharedUIDir()
	appDir := envOr("APP_DIR", ".")
	settingsPath := envOr("SETTINGS_FILE", filepath.Join(appDir, settingsFile))
	seedSettingsFromEnv(settingsPath)

	mux := http.NewServeMux()
	mux.Handle("/common/", http.StripPrefix("/common/", http.FileServer(http.Dir(filepath.Join(sharedDir, "common")))))
	mux.Handle("/i18n/", http.StripPrefix("/i18n/", http.FileServer(http.Dir(filepath.Join(sharedDir, "i18n")))))
	mux.Handle("/images/", http.StripPrefix("/images/", http.FileServer(http.Dir(filepath.Join(sharedDir, "images")))))
	mux.HandleFunc("/header-controls.json", localFile(filepath.Join(appDir, "header-controls.json")))
	mux.HandleFunc("/common/header-controls.json", localFile(filepath.Join(appDir, "header-controls.json")))
	mux.HandleFunc("/styles-modern.css", sharedStyle(sharedDir, "style-modern.css"))
	mux.HandleFunc("/styles-classic.css", sharedStyle(sharedDir, "style-classic.css"))
	mux.HandleFunc("/styles-glass.css", sharedStyle(sharedDir, "style-glass.css"))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{OK: true, Service: "fancontrol-config-gui"})
	})
	mux.HandleFunc("/api/config", configHandler(settingsPath))
	mux.HandleFunc("/api/settings.json", configHandler(settingsPath))
	mux.HandleFunc("/styles.css", localFile(filepath.Join(appDir, "styles.css")))
	mux.HandleFunc("/app.js", localFile(filepath.Join(appDir, "app.js")))
	mux.HandleFunc("/", serveApp(filepath.Join(appDir, "index.html")))

	server := &http.Server{Addr: ":" + port, Handler: logRequests(mux)}
	log.Printf("fancontrol GUI listening on http://127.0.0.1:%s", port)
	log.Printf("using mikrotik-ui-shared from %s", sharedDir)
	log.Fatal(server.ListenAndServe())
}

func configHandler(settingsPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settingsMu.Lock()
		defer settingsMu.Unlock()

		if r.Method == http.MethodGet {
			data, err := os.ReadFile(settingsPath)
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusOK, map[string]any{})
				return
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(data)
			return
		}

		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var settings map[string]any
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			http.Error(w, "invalid settings JSON", http.StatusBadRequest)
			return
		}
		data, err := json.MarshalIndent(settings, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		data = append(data, '\n')
		tmp := settingsPath + ".tmp"
		if err := os.WriteFile(tmp, data, 0600); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.Rename(tmp, settingsPath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "settings": settings})
	}
}

func seedSettingsFromEnv(settingsPath string) {
	keys := []string{"PROXMOX_SSH_HOST", "PROXMOX_SSH_PORT", "PROXMOX_SSH_USER", "PROXMOX_SSH_AUTH_METHOD", "PROXMOX_SSH_KEY", "PROXMOX_SSH_PASSWORD", "FANCONTROL_MODE", "FANCONTROL_ENABLED"}
	hasValues := false
	for _, key := range keys {
		if os.Getenv(key) != "" {
			hasValues = true
			break
		}
	}
	if !hasValues {
		return
	}

	settingsMu.Lock()
	defer settingsMu.Unlock()
	settings := map[string]any{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	ssh, _ := settings["ssh"].(map[string]any)
	if ssh == nil {
		ssh = map[string]any{}
	}
	setEnv(ssh, "host", "PROXMOX_SSH_HOST")
	setEnv(ssh, "port", "PROXMOX_SSH_PORT")
	setEnv(ssh, "user", "PROXMOX_SSH_USER")
	setEnv(ssh, "auth_method", "PROXMOX_SSH_AUTH_METHOD")
	setEnv(ssh, "private_key", "PROXMOX_SSH_KEY")
	setEnv(ssh, "password", "PROXMOX_SSH_PASSWORD")
	settings["ssh"] = ssh
	setEnv(settings, "mode", "FANCONTROL_MODE")
	setEnv(settings, "enabled", "FANCONTROL_ENABLED")
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		log.Printf("cannot encode environment settings: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0700); err != nil && filepath.Dir(settingsPath) != "." {
		log.Printf("cannot create settings directory: %v", err)
		return
	}
	if err := os.WriteFile(settingsPath, append(data, '\n'), 0600); err != nil {
		log.Printf("cannot write environment settings: %v", err)
	}
}

func setEnv(values map[string]any, field, envKey string) {
	if value := os.Getenv(envKey); value != "" {
		values[field] = value
	}
}

func serveApp(indexPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, indexPath)
	}
}

func sharedStyle(sharedDir, filename string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(sharedDir, "css", filename))
	}
}

func localFile(filename string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filename)
	}
}

func sharedUIDir() string {
	if dir := os.Getenv("UI_SHARED_DIR"); dir != "" {
		return dir
	}
	for _, candidate := range []string{
		"../mikrotik-ui-shared/ui",
		"/opt/mikrotik-ui-shared/ui",
	} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	log.Fatal("mikrotik-ui-shared not found; set UI_SHARED_DIR to its ui directory")
	return ""
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
