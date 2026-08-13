package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

var settingsMu sync.Mutex

//go:embed scripts/fancontrol-gui.sh scripts/fancontrol-gui.service
var hostControllerFiles embed.FS

const settingsFile = "settings.json"

type healthResponse struct {
	OK      bool   `json:"ok"`
	Service string `json:"service"`
}

type fanInfo struct {
	ID    string `json:"id"`
	RPM   string `json:"rpm"`
	TempC string `json:"temp_c"`
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
	mux.HandleFunc("/api/settings.json", uiSettingsHandler(settingsPath))
	mux.HandleFunc("/api/settings", uiSettingsHandler(settingsPath))
	mux.HandleFunc("/api/test-ssh", testSSHHandler(settingsPath))
	mux.HandleFunc("/api/fans", fansHandler(settingsPath))
	mux.HandleFunc("/api/apply", applyHandler(settingsPath))
	mux.HandleFunc("/api/off", offHandler(settingsPath))
	mux.HandleFunc("/api/restart", restartHandler(settingsPath))
	mux.HandleFunc("/styles.css", localFile(filepath.Join(appDir, "styles.css")))
	mux.HandleFunc("/app.js", localFile(filepath.Join(appDir, "app.js")))
	mux.HandleFunc("/", serveApp(filepath.Join(appDir, "index.html")))

	server := &http.Server{Addr: ":" + port, Handler: logRequests(mux)}
	log.Printf("fancontrol GUI listening on http://127.0.0.1:%s", port)
	log.Printf("using mikrotik-ui-shared from %s", sharedDir)
	log.Fatal(server.ListenAndServe())
}

func fansHandler(settingsPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		settings := loadSettingsFile(settingsPath)
		sshSettings, _ := settings["ssh"].(map[string]any)
		client, err := sshDial(sshSettings)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		defer client.Close()
		command := `for hw in /sys/class/hwmon/hwmon*; do
  [ -d "$hw" ] || continue
  for fan in "$hw"/fan*_input; do
    [ -r "$fan" ] || continue
    pwm="${fan%_input}"; pwm="${pwm/fan/pwm}"
    [ -w "$pwm" ] || continue
    rpm=$(cat "$fan" 2>/dev/null || echo 0)
    temp=$(cat "$hw"/temp*_input 2>/dev/null | head -1 || echo 0)
    printf '%s|%s|%s\n' "${fan##*/}" "$rpm" "$temp"
  done
done`
		output, err := sshCommand(client, command)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": output + " " + err.Error()})
			return
		}
		fans := make([]fanInfo, 0)
		for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
			parts := strings.Split(line, "|")
			if len(parts) != 3 || parts[0] == "" {
				continue
			}
			temp := parts[2]
			if value, parseErr := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64); parseErr == nil {
				temp = fmt.Sprintf("%.1f", value/1000)
			}
			fans = append(fans, fanInfo{ID: parts[0], RPM: parts[1], TempC: temp})
		}
		jsonResponse(w, http.StatusOK, map[string]any{"ok": true, "fans": fans})
	}
}

func testSSHHandler(settingsPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var settings map[string]any
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			http.Error(w, "invalid settings JSON", http.StatusBadRequest)
			return
		}
		if len(settings) == 0 {
			data, _ := os.ReadFile(settingsPath)
			_ = json.Unmarshal(data, &settings)
		}
		sshSettings, _ := settings["ssh"].(map[string]any)
		client, err := sshDial(sshSettings)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		defer client.Close()
		output, err := sshCommand(client, "hostname && (systemctl is-active fancontrol-gui.service 2>/dev/null || systemctl is-active fancontrol 2>/dev/null || true)")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": strings.TrimSpace(output + " " + err.Error())})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": output})
	}
}

func sshDial(settings map[string]any) (*ssh.Client, error) {
	method := stringSetting(settings, "auth_method", "key")
	var auth ssh.AuthMethod
	if method == "password" {
		auth = ssh.Password(stringSetting(settings, "password", ""))
	} else {
		signer, err := ssh.ParsePrivateKey([]byte(stringSetting(settings, "private_key", "")))
		if err != nil {
			return nil, fmt.Errorf("private key: %w", err)
		}
		auth = ssh.PublicKeys(signer)
	}
	port := intSetting(settings, "port", 22)
	config := &ssh.ClientConfig{User: stringSetting(settings, "user", "root"), Auth: []ssh.AuthMethod{auth}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 8 * time.Second}
	return ssh.Dial("tcp", fmt.Sprintf("%s:%d", stringSetting(settings, "host", ""), port), config)
}

func sshCommand(client *ssh.Client, command string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	output, err := session.CombinedOutput(command)
	return strings.TrimSpace(string(output)), err
}

func sshCommandInput(client *ssh.Client, command, input string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	session.Stdin = strings.NewReader(input)
	output, err := session.CombinedOutput(command)
	return strings.TrimSpace(string(output)), err
}

func hostMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "fullfan", "full fan":
		return "0"
	case "quietfan", "quiet fan":
		return "2"
	default:
		return "1"
	}
}

func curveLine(curve any, row int) string {
	rows, ok := curve.([]any)
	if !ok || row >= len(rows) {
		return "20 20 40 40 60 60"
	}
	values, ok := rows[row].([]any)
	if !ok || len(values) < 6 {
		return "20 20 40 40 60 60"
	}
	parts := make([]string, 6)
	for i := range parts {
		parts[i] = fmt.Sprintf("%d", intSetting(map[string]any{"v": values[i]}, "v", 0))
	}
	return strings.Join(parts, " ")
}

func hostControllerConfig(settings map[string]any) string {
	selected := make([]string, 0, 3)
	if fans, ok := settings["fans"].([]any); ok {
		for _, fan := range fans {
			if enabled, ok := fan.(bool); ok && enabled {
				selected = append(selected, "1")
			} else {
				selected = append(selected, "0")
			}
		}
	}
	for len(selected) < 3 {
		selected = append(selected, "1")
	}
	return fmt.Sprintf("# Generated by fancontrol-config-gui. Do not edit while the GUI is applying settings.\nENABLED=%s\nACTIVE_MODE=%s\nINTERVAL=2\nFAN_SELECTION=\"%s\"\nCURVE_FULL=\"%s\"\nCURVE_COOL=\"%s\"\nCURVE_QUIET=\"%s\"\n", boolNumber(settings["enabled"]), hostMode(stringSetting(settings, "mode", "coolfan")), strings.Join(selected, " "), curveLine(settings["curve"], 0), curveLine(settings["curve"], 1), curveLine(settings["curve"], 2))
}

func boolNumber(value any) string {
	if enabled, ok := value.(bool); ok && enabled {
		return "1"
	}
	return "0"
}

func shellQuote(value string) string                            { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
func jsonResponse(w http.ResponseWriter, status int, value any) { writeJSON(w, status, value) }

func sshSettingsFrom(settings map[string]any) (map[string]any, error) {
	sshSettings, ok := settings["ssh"].(map[string]any)
	if !ok || sshSettings == nil {
		return nil, fmt.Errorf("SSH settings are missing")
	}
	return sshSettings, nil
}

func applyRemote(client *ssh.Client, settings map[string]any) (string, error) {
	stamp := time.Now().UTC().Format("20060102-150405")
	backup := "/root/fancontrol-gui-backup-" + stamp
	if _, err := sshCommand(client, "mkdir -p "+shellQuote(backup)+" /usr/local/sbin /etc/systemd/system && for f in /etc/fancontrol-gui.conf /usr/local/sbin/fancontrol-gui /etc/systemd/system/fancontrol-gui.service; do test ! -e \"$f\" || cp -a \"$f\" "+shellQuote(backup)+"/; done"); err != nil {
		return "", err
	}
	files := map[string]string{"/etc/fancontrol-gui.conf": hostControllerConfig(settings)}
	for _, name := range []string{"scripts/fancontrol-gui.sh", "scripts/fancontrol-gui.service"} {
		data, err := hostControllerFiles.ReadFile(name)
		if err != nil {
			return "", err
		}
		if name == "scripts/fancontrol-gui.sh" {
			files["/usr/local/sbin/fancontrol-gui"] = string(data)
		} else {
			files["/etc/systemd/system/fancontrol-gui.service"] = string(data)
		}
	}
	for path, content := range files {
		tmp := path + ".tmp-fancontrol-gui"
		if _, err := sshCommandInput(client, "umask 077; cat > "+shellQuote(tmp)+" && mv "+shellQuote(tmp)+" "+shellQuote(path), content); err != nil {
			return "", err
		}
	}
	if _, err := sshCommand(client, "chmod 755 /usr/local/sbin/fancontrol-gui && (systemctl disable --now fancontrol.service 2>/dev/null || true) && systemctl daemon-reload && if [ \""+boolNumber(settings["enabled"])+"\" = 1 ]; then systemctl enable --now fancontrol-gui.service && systemctl restart fancontrol-gui.service; else systemctl disable --now fancontrol-gui.service 2>/dev/null || true; fi"); err != nil {
		return "", err
	}
	status, err := sshCommand(client, "systemctl is-active fancontrol-gui.service || true")
	return backup + "\nservice=" + status, err
}

func applyHandler(settingsPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		settings := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid settings JSON"})
			return
		}
		sshSettings, err := sshSettingsFrom(settings)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		client, err := sshDial(sshSettings)
		if err != nil {
			jsonResponse(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		defer client.Close()
		result, err := applyRemote(client, settings)
		if err != nil {
			jsonResponse(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if err := saveSettingsFile(settingsPath, settings); err != nil {
			jsonResponse(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		jsonResponse(w, 200, map[string]any{"ok": true, "result": result})
	}
}

func offHandler(settingsPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		settings := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid settings JSON"})
			return
		}
		settings["enabled"] = false
		sshSettings, err := sshSettingsFrom(settings)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		client, err := sshDial(sshSettings)
		if err != nil {
			jsonResponse(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		defer client.Close()
		result, err := applyRemote(client, settings)
		if err != nil {
			jsonResponse(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if err := saveSettingsFile(settingsPath, settings); err != nil {
			jsonResponse(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		jsonResponse(w, 200, map[string]any{"ok": true, "result": result})
	}
}

func restartHandler(settingsPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		settings := loadSettingsFile(settingsPath)
		sshSettings, err := sshSettingsFrom(settings)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		client, err := sshDial(sshSettings)
		if err != nil {
			jsonResponse(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		defer client.Close()
		output, err := sshCommand(client, "systemctl restart fancontrol-gui.service && systemctl is-active fancontrol-gui.service")
		if err != nil {
			jsonResponse(w, 400, map[string]any{"ok": false, "error": output + " " + err.Error()})
			return
		}
		jsonResponse(w, 200, map[string]any{"ok": true, "output": output})
	}
}

func loadSettingsFile(path string) map[string]any {
	settings := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	return settings
}
func saveSettingsFile(path string, settings map[string]any) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil && filepath.Dir(path) != "." {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

func stringSetting(values map[string]any, key, fallback string) string {
	if value, ok := values[key].(string); ok && value != "" {
		return value
	}
	return fallback
}
func intSetting(values map[string]any, key string, fallback int) int {
	if value, ok := values[key].(float64); ok && value > 0 {
		return int(value)
	}
	return fallback
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

func uiSettingsHandler(settingsPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settingsMu.Lock()
		defer settingsMu.Unlock()
		all := map[string]any{}
		if data, err := os.ReadFile(settingsPath); err == nil {
			_ = json.Unmarshal(data, &all)
		}
		ui, _ := all["ui_settings"].(map[string]any)
		if ui == nil {
			ui = map[string]any{}
		}
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, ui)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var patch map[string]any
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			http.Error(w, "invalid UI settings JSON", http.StatusBadRequest)
			return
		}
		for key, value := range patch {
			ui[key] = value
		}
		all["ui_settings"] = ui
		data, err := json.MarshalIndent(all, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile(settingsPath, append(data, '\n'), 0600); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "settings": ui})
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
