package web

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"bili-subtitle/internal/config"
)

// presetInfo 是提示词预设文件的列表项。
type presetInfo struct {
	Name string `json:"name"`
	File string `json:"file"`
}

// presetDir 返回提示词所在目录（预设文件都放这里）。
func presetDir(cfg *config.Config) string {
	return filepath.Dir(cfg.Resolve(cfg.PromptFile))
}

// presetNameOf 返回当前默认提示词对应的预设名。
func presetNameOf(cfg *config.Config) string {
	base := filepath.Base(cfg.Resolve(cfg.PromptFile))
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// listPresets 列出目录里的 .md 预设，按名字排序。
func listPresets(cfg *config.Config) []presetInfo {
	entries, err := os.ReadDir(presetDir(cfg))
	if err != nil {
		return []presetInfo{}
	}
	out := make([]presetInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		out = append(out, presetInfo{Name: strings.TrimSuffix(name, filepath.Ext(name)), File: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// sanitizePresetName 清洗用户输入的预设名，只保留文件名部分。
func sanitizePresetName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if strings.HasSuffix(strings.ToLower(name), ".md") {
		name = strings.TrimSpace(name[:len(name)-3])
	}
	name = strings.Map(func(r rune) rune {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" || name == "." || name == ".." {
		return "", errors.New("预设名不合法")
	}
	return name, nil
}

func (s *Server) handlePresets(w http.ResponseWriter, r *http.Request) {
	cfg := s.snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"presets":       listPresets(cfg),
		"currentPreset": presetNameOf(cfg),
	})
}

func (s *Server) handlePreset(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := s.snapshot()
		name, err := sanitizePresetName(r.URL.Query().Get("name"))
		if err != nil {
			writeError(w, err)
			return
		}
		path := filepath.Join(presetDir(cfg), name+".md")
		data, err := os.ReadFile(path)
		if err != nil {
			writeError(w, fmt.Errorf("读预设 %s 失败：%w", name, err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name, "text": string(data)})
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
			Text string `json:"text"`
		}
		if err := readJSON(r, &req); err != nil {
			writeError(w, err)
			return
		}
		name, err := sanitizePresetName(req.Name)
		if err != nil {
			writeError(w, err)
			return
		}
		if strings.TrimSpace(req.Text) == "" {
			writeError(w, errors.New("预设内容是空的"))
			return
		}
		cfg := s.snapshot()
		dir := presetDir(cfg)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			writeError(w, err)
			return
		}
		path := filepath.Join(dir, name+".md")
		if err := os.WriteFile(path, []byte(req.Text), 0o644); err != nil {
			writeError(w, fmt.Errorf("写预设失败：%w", err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":            true,
			"name":          name,
			"path":          path,
			"presets":       listPresets(cfg),
			"currentPreset": presetNameOf(cfg),
		})
	default:
		http.Error(w, "只支持 GET/POST", http.StatusMethodNotAllowed)
	}
}
