// Package web 提供只监听回环地址的本地图形界面：启动时生成随机 token，
// 浏览器打开后调用同一进程里的取字幕/总结流程。
package web

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"bili-subtitle/internal/app"
	"bili-subtitle/internal/config"
	"bili-subtitle/internal/note"
	"bili-subtitle/internal/transcript"
)

//go:embed static
var staticFS embed.FS

const maxBodyBytes = 8 << 20

// Server 持有界面需要的配置和 token。
type Server struct {
	cfg     *config.Config
	cfgPath string
	token   string
	mu      sync.Mutex
}

// New 创建界面服务；token 自动生成。
func New(cfg *config.Config, cfgPath string) *Server {
	return &Server{cfg: cfg, cfgPath: cfgPath, token: randomToken()}
}

func randomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

// Run 监听 127.0.0.1 的随机（或指定）端口，打印地址并按需打开浏览器。
func (s *Server) Run(port int, openBrowser bool) error {
	addr := "127.0.0.1:" + strconv.Itoa(port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("监听 %s 失败：%w", addr, err)
	}
	url := "http://" + ln.Addr().String() + "/?token=" + s.token
	fmt.Println("本地界面：" + url)
	fmt.Println("在这个窗口按 Ctrl+C 退出")
	if openBrowser {
		_ = exec.Command("cmd", "/c", "start", "", url).Start()
	}
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	err = srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Handler 返回路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	staticRoot, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticRoot))))
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", s.auth(s.handleIndex))
	mux.HandleFunc("/api/state", s.auth(s.handleState))
	mux.HandleFunc("/api/config", s.auth(s.handleConfig))
	mux.HandleFunc("/api/prompt", s.auth(s.handlePrompt))
	mux.HandleFunc("/api/fetch", s.auth(s.handleFetch))
	mux.HandleFunc("/api/summarize", s.auth(s.handleSummarize))
	mux.HandleFunc("/api/open", s.auth(s.handleOpen))
	mux.HandleFunc("/api/open-dir", s.auth(s.handleOpenDir))
	mux.HandleFunc("/api/history", s.auth(s.handleHistory))
	return mux
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := r.URL.Query().Get("token")
		if tok == "" {
			tok = r.Header.Get("X-Bili-Sub-Token")
		}
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) != 1 {
			http.Error(w, "token 不对，请从启动时打印的地址打开", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// ---------- 页面与静态资源 ----------

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// ---------- API ----------

type configView struct {
	OutputDir         string   `json:"outputDir"`
	Summarizer        string   `json:"summarizer"`
	Model             string   `json:"model"`
	OpencodeModel     string   `json:"opencodeModel"`
	OpenWith          string   `json:"openWith"`
	TyporaPath        string   `json:"typoraPath"`
	PromptFile        string   `json:"promptFile"`
	TemplateFile      string   `json:"templateFile"`
	SubLangs          []string `json:"subLangs"`
	OnExists          string   `json:"onExists"`
	MaxChars          int      `json:"maxChars"`
	KeepSubtitle      bool     `json:"keepSubtitle"`
	IncludeTranscript bool     `json:"includeTranscript"`
	APIKeySet         bool     `json:"apiKeySet"`
}

func viewOf(cfg *config.Config) configView {
	return configView{
		OutputDir:         cfg.OutputDir,
		Summarizer:        cfg.Summarizer,
		Model:             cfg.Model,
		OpencodeModel:     cfg.OpencodeModel,
		OpenWith:          cfg.OpenWith,
		TyporaPath:        cfg.TyporaPath,
		PromptFile:        cfg.PromptFile,
		TemplateFile:      cfg.TemplateFile,
		SubLangs:          append([]string(nil), cfg.SubLangs...),
		OnExists:          cfg.OnExists,
		MaxChars:          cfg.MaxChars,
		KeepSubtitle:      cfg.KeepSubtitle,
		IncludeTranscript: cfg.IncludeTranscript,
		APIKeySet:         strings.TrimSpace(cfg.APIKey) != "",
	}
}

func (s *Server) snapshot() *config.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *s.cfg
	cp.SubLangs = append([]string(nil), s.cfg.SubLangs...)
	return &cp
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	cfg := s.snapshot()
	s.mu.Lock()
	cfgPath := s.cfgPath
	s.mu.Unlock()
	prompt := ""
	if b, err := os.ReadFile(cfg.Resolve(cfg.PromptFile)); err == nil {
		prompt = string(b)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"configPath": cfgPath,
		"promptPath": cfg.Resolve(cfg.PromptFile),
		"prompt":     prompt,
		"config":     viewOf(cfg),
		"history":    s.loadHistory(20),
	})
}

type configPatch struct {
	OutputDir         *string `json:"outputDir"`
	Summarizer        *string `json:"summarizer"`
	Model             *string `json:"model"`
	OpencodeModel     *string `json:"opencodeModel"`
	OpenWith          *string `json:"openWith"`
	TyporaPath        *string `json:"typoraPath"`
	OnExists          *string `json:"onExists"`
	KeepSubtitle      *bool   `json:"keepSubtitle"`
	IncludeTranscript *bool   `json:"includeTranscript"`
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只支持 POST", http.StatusMethodNotAllowed)
		return
	}
	var patch configPatch
	if err := readJSON(r, &patch); err != nil {
		writeError(w, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if patch.OutputDir != nil {
		s.cfg.OutputDir = *patch.OutputDir
	}
	if patch.Summarizer != nil {
		s.cfg.Summarizer = *patch.Summarizer
	}
	if patch.Model != nil {
		s.cfg.Model = *patch.Model
	}
	if patch.OpencodeModel != nil {
		s.cfg.OpencodeModel = *patch.OpencodeModel
	}
	if patch.OpenWith != nil {
		s.cfg.OpenWith = *patch.OpenWith
	}
	if patch.TyporaPath != nil {
		s.cfg.TyporaPath = *patch.TyporaPath
	}
	if patch.OnExists != nil {
		s.cfg.OnExists = *patch.OnExists
	}
	if patch.KeepSubtitle != nil {
		s.cfg.KeepSubtitle = *patch.KeepSubtitle
	}
	if patch.IncludeTranscript != nil {
		s.cfg.IncludeTranscript = *patch.IncludeTranscript
	}
	if err := s.cfg.Save(s.cfgPath); err != nil {
		writeError(w, fmt.Errorf("保存配置失败：%w", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": viewOf(s.cfg)})
}

func (s *Server) handlePrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只支持 POST", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	s.mu.Lock()
	path := s.cfg.Resolve(s.cfg.PromptFile)
	s.mu.Unlock()
	if err := os.WriteFile(path, []byte(req.Text), 0o644); err != nil {
		writeError(w, fmt.Errorf("写入提示词失败：%w", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": path})
}

type fetchRequest struct {
	Input  string   `json:"input"`
	Engine string   `json:"engine"`
	Langs  []string `json:"langs"`
}

func (s *Server) handleFetch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只支持 POST", http.StatusMethodNotAllowed)
		return
	}
	var req fetchRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(req.Input) == "" {
		writeError(w, errors.New("先填 BV 号或视频链接"))
		return
	}
	cfg := s.snapshot()
	logs := make([]string, 0, 8)
	logf := func(format string, a ...any) { logs = append(logs, fmt.Sprintf(format, a...)) }
	fetched, err := app.Fetch(r.Context(), cfg, app.FetchOptions{Input: req.Input, Engine: req.Engine, Langs: req.Langs}, logf)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "logs": logs})
		return
	}
	srt := transcript.ToSRT(fetched.Cues)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"video": map[string]any{
			"title":    fetched.Video.Title,
			"owner":    fetched.Video.Owner,
			"bvid":     fetched.Video.BVid,
			"duration": app.HumanDuration(fetched.Video.Duration),
			"page":     fetched.Video.Page,
			"url":      fetched.Video.URL,
		},
		"lang":     fetched.Lang,
		"cueCount": len(fetched.Cues),
		"chars":    len([]rune(fetched.Text)),
		"text":     fetched.Text,
		"srt":      srt,
		"logs":     logs,
	})
}

type summarizeRequest struct {
	Input             string `json:"input"`
	Prompt            string `json:"prompt"`
	Summarizer        string `json:"summarizer"`
	Model             string `json:"model"`
	OpencodeModel     string `json:"opencodeModel"`
	OutDir            string `json:"outDir"`
	OpenWith          string `json:"openWith"`
	KeepSubtitle      bool   `json:"keepSubtitle"`
	IncludeTranscript bool   `json:"includeTranscript"`
}

func (s *Server) handleSummarize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只支持 POST", http.StatusMethodNotAllowed)
		return
	}
	var req summarizeRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(req.Input) == "" {
		writeError(w, errors.New("先填 BV 号或视频链接"))
		return
	}
	cfg := s.snapshot()
	logs := make([]string, 0, 12)
	logf := func(format string, a ...any) { logs = append(logs, fmt.Sprintf(format, a...)) }
	res, err := app.Run(r.Context(), cfg, app.RunOptions{
		FetchOptions:      app.FetchOptions{Input: req.Input},
		Prompt:            req.Prompt,
		Summarizer:        req.Summarizer,
		Model:             req.Model,
		OpencodeModel:     req.OpencodeModel,
		OutDir:            req.OutDir,
		OpenWith:          req.OpenWith,
		KeepSubtitle:      req.KeepSubtitle,
		IncludeTranscript: req.IncludeTranscript,
	}, logf)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "logs": logs})
		return
	}
	entry := historyEntry{
		Time:    time.Now().Format("2006-01-02 15:04"),
		Input:   req.Input,
		BVID:    res.Fetched.Video.BVid,
		Title:   res.Fetched.Video.Title,
		Owner:   res.Fetched.Video.Owner,
		Path:    res.Path,
		Backend: summarizeBackend(req, cfg),
	}
	s.appendHistory(entry)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"path":      res.Path,
		"summary":   res.Summary,
		"chars":     len([]rune(res.Summary)),
		"srtPath":   res.SRTPath,
		"elapsedMs": res.Elapsed.Milliseconds(),
		"video": map[string]any{
			"title": res.Fetched.Video.Title,
			"owner": res.Fetched.Video.Owner,
			"bvid":  res.Fetched.Video.BVid,
		},
		"logs": logs,
	})
}

func summarizeBackend(req summarizeRequest, cfg *config.Config) string {
	s := req.Summarizer
	if s == "" {
		s = cfg.Summarizer
	}
	if s == "opencode" {
		m := req.OpencodeModel
		if m == "" {
			m = cfg.OpencodeModel
		}
		return "opencode:" + m
	}
	m := req.Model
	if m == "" {
		m = cfg.Model
	}
	return "api:" + m
}

func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只支持 POST", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Path string `json:"path"`
		How  string `json:"how"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	if _, err := os.Stat(req.Path); err != nil {
		writeError(w, fmt.Errorf("文件不存在：%s", req.Path))
		return
	}
	cfg := s.snapshot()
	cfg.OpenWith = req.How
	if err := note.Open(req.Path, cfg); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleOpenDir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只支持 POST", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	abs, err := filepath.Abs(req.Path)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := exec.Command("explorer.exe", "/select,"+abs).Start(); err != nil {
		writeError(w, fmt.Errorf("打开目录失败：%w", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- 历史记录 ----------

type historyEntry struct {
	Time    string `json:"time"`
	Input   string `json:"input"`
	BVID    string `json:"bvid"`
	Title   string `json:"title"`
	Owner   string `json:"owner"`
	Path    string `json:"path"`
	Backend string `json:"backend"`
}

func (s *Server) historyPath() string {
	cfg := s.snapshot()
	return cfg.Resolve("data/history.jsonl")
}

func (s *Server) appendHistory(e historyEntry) {
	path := s.historyPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
}

func (s *Server) loadHistory(limit int) []historyEntry {
	path := s.historyPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return []historyEntry{}
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	entries := make([]historyEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e historyEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	// 新的放前面
	out := make([]historyEntry, 0, limit)
	for i := len(entries) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, entries[i])
	}
	return out
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "history": s.loadHistory(20)})
}

// ---------- JSON 小工具 ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("读取请求失败：%w", err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("请求不是合法 JSON：%w", err)
	}
	return nil
}
