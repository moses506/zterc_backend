package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"zetrc-backend/internal/httpx"
)

// UploadHandler stores enrollment/document uploads (ID copies, certificates,
// photos, etc.) on local disk and serves them back under /uploads/.
type UploadHandler struct {
	uploadDir       string
	maxFileSizeByte int64
}

func NewUploadHandler(uploadDir string, maxFileSizeMB int64) *UploadHandler {
	return &UploadHandler{
		uploadDir:       uploadDir,
		maxFileSizeByte: maxFileSizeMB * 1024 * 1024,
	}
}

type uploadedFile struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

var allowedUploadExtensions = map[string]bool{
	".pdf": true, ".png": true, ".jpg": true, ".jpeg": true, ".doc": true, ".docx": true,
	".mp3": true, ".wav": true, ".m4a": true, ".amr": true, ".ogg": true, ".aac": true,
	// Browser voice recordings and phone photos.
	".webm": true, ".mp4": true, ".webp": true,
}

// audioNeedsTranscode lists uploaded audio formats no browser can play
// natively (e.g. AMR is a phone voice-call codec, not a web format). These
// are transcoded to MP3 server-side via ffmpeg immediately after upload so
// lesson audio is always playable in the training view.
var audioNeedsTranscode = map[string]bool{
	".amr": true,
}

// transcodeToMP3 converts srcPath to an MP3 sitting next to it (same name,
// .mp3 extension) using ffmpeg. Requires ffmpeg to be on PATH.
func transcodeToMP3(srcPath string) (string, error) {
	dstPath := strings.TrimSuffix(srcPath, filepath.Ext(srcPath)) + ".mp3"

	cmd := exec.Command("ffmpeg", "-y", "-i", srcPath, "-codec:a", "libmp3lame", "-qscale:a", "4", dstPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return dstPath, nil
}

// POST /api/upload-file/ — multipart/form-data with one or more files under
// the "file" or "files" field.
func (h *UploadHandler) Upload(w http.ResponseWriter, r *http.Request) {
	const maxFilesPerRequest = 6
	r.Body = http.MaxBytesReader(w, r.Body, h.maxFileSizeByte*maxFilesPerRequest)

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "Upload too large or invalid form data.")
		return
	}

	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		files = r.MultipartForm.File["files"]
	}
	if len(files) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "No file provided under 'file' or 'files'.")
		return
	}
	if len(files) > maxFilesPerRequest {
		httpx.WriteError(w, http.StatusBadRequest, "Too many files in one request.")
		return
	}

	if err := os.MkdirAll(h.uploadDir, 0o755); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "Could not prepare upload storage.")
		return
	}

	results := make([]uploadedFile, 0, len(files))
	for _, fh := range files {
		if fh.Size > h.maxFileSizeByte {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, fh.Filename+" exceeds the maximum upload size.")
			return
		}

		ext := strings.ToLower(filepath.Ext(fh.Filename))
		if !allowedUploadExtensions[ext] {
			httpx.WriteError(w, http.StatusUnsupportedMediaType, "Unsupported file type: "+ext)
			return
		}

		src, err := fh.Open()
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "Could not read uploaded file.")
			return
		}

		storedName := randomFilename() + ext
		dest, err := os.Create(filepath.Join(h.uploadDir, storedName))
		if err != nil {
			src.Close()
			httpx.WriteError(w, http.StatusInternalServerError, "Could not store uploaded file.")
			return
		}

		written, err := io.Copy(dest, src)
		src.Close()
		dest.Close()
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "Could not store uploaded file.")
			return
		}

		servedName := storedName
		servedSize := written

		if audioNeedsTranscode[ext] {
			srcPath := filepath.Join(h.uploadDir, storedName)
			if mp3Path, tErr := transcodeToMP3(srcPath); tErr != nil {
				log.Printf("audio transcode failed for %q: %v (serving original format)", fh.Filename, tErr)
			} else if info, statErr := os.Stat(mp3Path); statErr != nil {
				log.Printf("transcoded file missing for %q: %v (serving original format)", fh.Filename, statErr)
			} else {
				_ = os.Remove(srcPath)
				servedName = strings.TrimSuffix(storedName, ext) + ".mp3"
				servedSize = info.Size()
			}
		}

		results = append(results, uploadedFile{
			Name: fh.Filename,
			URL:  "/uploads/" + servedName,
			Size: servedSize,
		})
	}

	httpx.WriteData(w, http.StatusCreated, results)
}

func randomFilename() string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
