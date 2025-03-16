package main

import (
    "crypto/rand"
    "encoding/base64"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"

    "github.com/go-chi/chi/v5"
    "github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
    videoIDStr := chi.URLParam(r, "videoID")
    videoID, err := uuid.Parse(videoIDStr)
    if err != nil {
        respondWithError(w, http.StatusBadRequest, "Invalid video ID", err)
        return
    }

    userIDStr, ok := r.Context().Value("userID").(string)
    if !ok {
        respondWithError(w, http.StatusInternalServerError, "Couldn't find userID in context", nil)
        return
    }
    userID, err := uuid.Parse(userIDStr)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Invalid userID format", err)
        return
    }

    err = r.ParseMultipartForm(10 << 20) // 10 MB limit
    if err != nil {
        respondWithError(w, http.StatusBadRequest, "Unable to parse form", err)
        return
    }

    file, header, err := r.FormFile("thumbnail")
    if err != nil {
        respondWithError(w, http.StatusBadRequest, "Missing thumbnail file", err)
        return
    }
    defer file.Close()

    // Determine file extension from Content-Type
    mediaType := header.Header.Get("Content-Type")
    var ext string
    switch mediaType {
    case "image/png":
        ext = ".png"
    case "image/jpeg", "image/jpg":
        ext = ".jpg"
    default:
        ext = ".png" // Fallback
    }

    // Generate a random 32-byte slice
    randomBytes := make([]byte, 32)
    _, err = rand.Read(randomBytes)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Couldn't generate random filename", err)
        return
    }

    // Encode to base64 URL-safe string
    fileNameBase := base64.RawURLEncoding.EncodeToString(randomBytes)
    fileName := fileNameBase + ext

    // Create file path: /assets/<randomBase64>.<ext>
    filePath := filepath.Join(cfg.assetsRoot, "assets", fileName)
    if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
        respondWithError(w, http.StatusInternalServerError, "Couldn't create assets directory", err)
        return
    }

    // Create and write to file
    outFile, err := os.Create(filePath)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Couldn't create file", err)
        return
    }
    defer outFile.Close()

    _, err = io.Copy(outFile, file)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Failed to write thumbnail to disk", err)
        return
    }

    // Update video in database
    video, err := cfg.db.GetVideo(videoID)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Couldn't find video", err)
        return
    }
    if video.UserID != userID {
        respondWithError(w, http.StatusForbidden, "You don't own this video", nil)
        return
    }

    thumbnailURL := fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, fileName)
    video.ThumbnailURL = &thumbnailURL
    err = cfg.db.UpdateVideo(video)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Couldn't update video", err)
        return
    }

    respondWithJSON(w, http.StatusOK, video)
}
