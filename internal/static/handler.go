package static

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"nexus/pkg/logger"

	"go.uber.org/zap"
)

type StaticHandler struct {
	domain     string
	staticPath string
	indexPath  string
}

func NewStaticHandler(domain, staticPath, indexPath string) *StaticHandler {
	return &StaticHandler{
		domain:     domain,
		staticPath: staticPath,
		indexPath:  indexPath,
	}
}

func (h *StaticHandler) Domain() string {
	return h.domain
}

func (h *StaticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/") {
		h.serveIndex(w, r)
		return
	}

	filePath := filepath.Join(h.staticPath, r.URL.Path)

	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			h.serveIndex(w, r)
			return
		}
		logger.Log.Error("Failed to stat file", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if info.IsDir() {
		h.serveIndex(w, r)
		return
	}

	http.ServeFile(w, r, filePath)
}

func (h *StaticHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	indexPath := filepath.Join(h.staticPath, h.indexPath)
	if _, err := os.Stat(indexPath); err != nil {
		logger.Log.Error("Index file not found", zap.String("path", indexPath))
		http.Error(w, "Index file not found", http.StatusNotFound)
		return
	}

	http.ServeFile(w, r, indexPath)
}
