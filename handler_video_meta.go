package main

import (
    "context"
    "crypto/rand"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "mime"
    "net/http"
    "os"
    "strings"


    "github.com/aws/aws-sdk-go-v2/service/s3"
    "github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
    "github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
    "github.com/go-chi/chi/v5"
    "github.com/google/uuid"
)

func (cfg *apiConfig) handlerVideoMetaCreate(w http.ResponseWriter, r *http.Request) {
    type parameters struct {
        database.CreateVideoParams
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

    decoder := json.NewDecoder(r.Body)
    params := parameters{}
    err = decoder.Decode(&params)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Couldn't decode parameters", err)
        return
    }
    params.UserID = userID

    video, err := cfg.db.CreateVideo(params.CreateVideoParams)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Couldn't create video", err)
        return
    }

    respondWithJSON(w, http.StatusCreated, video)
}

func (cfg *apiConfig) handlerVideoMetaDelete(w http.ResponseWriter, r *http.Request) {
    log.Printf("Using jwtSecret: %s", cfg.jwtSecret)
    videoIDString := r.PathValue("videoID")
    log.Printf("Attempting to delete video with ID: %s", videoIDString)
    videoID, err := uuid.Parse(videoIDString)
    if err != nil {
        log.Printf("Invalid video ID format: %v", err)
        respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
        return
    }

    token, err := auth.GetBearerToken(r.Header)
    if err != nil {
        log.Printf("No JWT token found: %v", err)
        respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
        return
    }
    log.Printf("Validating token: %s", token[:50]+"...")
    userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
    if err != nil {
        log.Printf("JWT validation failed: %v", err)
        respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
        return
    }
    log.Printf("Authenticated user ID: %s", userID)

    video, err := cfg.db.GetVideo(videoID)
    if err != nil {
        log.Printf("Failed to get video %s: %v", videoID, err)
        respondWithError(w, http.StatusNotFound, "Couldn't get video", err)
        return
    }
    log.Printf("Video found: %+v", video)

    if video.UserID != userID {
        log.Printf("User %s not authorized to delete video owned by %s", userID, video.UserID)
        respondWithError(w, http.StatusForbidden, "You can't delete this video", nil)
        return
    }

    if video.VideoURL != nil && *video.VideoURL != "" {
        parts := strings.Split(*video.VideoURL, "/")
        if len(parts) >= 4 {
            s3Key := strings.Join(parts[3:], "/")
            log.Printf("Deleting S3 object with key: %s", s3Key)
            _, err := cfg.s3Client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
                Bucket: &cfg.s3Bucket,
                Key:    &s3Key,
            })
            if err != nil {
                log.Printf("Failed to delete S3 object %s: %v", s3Key, err)
            }
        }
    }

    err = cfg.db.DeleteVideo(videoID)
    if err != nil {
        log.Printf("Failed to delete video %s: %v", videoID, err)
        respondWithError(w, http.StatusInternalServerError, "Couldn't delete video", err)
        return
    }

    log.Printf("Successfully deleted video %s", videoID)
    w.WriteHeader(http.StatusNoContent)
}


func (cfg *apiConfig) handlerVideoGet(w http.ResponseWriter, r *http.Request) {
    videoIDString := r.PathValue("videoID")
    videoID, err := uuid.Parse(videoIDString)
    if err != nil {
        respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
        return
    }

    userIDStr, ok := r.Context().Value("userID").(string)
    if !ok {
        respondWithError(w, http.StatusInternalServerError, "Couldn't find userID in context", nil)
        return
    }

    video, err := cfg.db.GetVideo(videoID)
    if err != nil {
        respondWithError(w, http.StatusNotFound, "Couldn't get video", err)
        return
    }
    if video.UserID.String() != userIDStr {
        respondWithError(w, http.StatusForbidden, "You can't view this video", nil)
        return
    }

    respondWithJSON(w, http.StatusOK, video) // Return the video directly
}



func (cfg *apiConfig) handlerVideosRetrieve(w http.ResponseWriter, r *http.Request) {
    log.Printf("handlerVideosRetrieve called for request: %s", r.URL.Path)

    userIDStr, ok := r.Context().Value("userID").(string)
    if !ok {
        log.Printf("Couldn't find userID in context")
        respondWithError(w, http.StatusInternalServerError, "Couldn't find userID in context", nil)
        return
    }
    log.Printf("UserID from context: %s", userIDStr)

    userID, err := uuid.Parse(userIDStr)
    if err != nil {
        log.Printf("Invalid userID format: %v", err)
        respondWithError(w, http.StatusInternalServerError, "Invalid userID format", err)
        return
    }

    videos, err := cfg.db.GetVideos(userID)
    if err != nil {
        log.Printf("Couldn't get videos: %v", err)
        respondWithError(w, http.StatusInternalServerError, "Couldn't get videos", err)
        return
    }
    log.Printf("Retrieved %d videos", len(videos))


    log.Printf("Returning %d videos", len(videos))
    respondWithJSON(w, http.StatusOK, videos) 
}


func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
    r.Body = http.MaxBytesReader(w, r.Body, 1<<30)

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

    video, err := cfg.db.GetVideo(videoID)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Couldn't find video", err)
        return
    }
    if video.UserID != userID {
        respondWithError(w, http.StatusUnauthorized, "You don't own this video", nil)
        return
    }

    err = r.ParseMultipartForm(1<<30)
    if err != nil {
        respondWithError(w, http.StatusBadRequest, "Unable to parse form", err)
        return
    }

    file, header, err := r.FormFile("video")
    if err != nil {
        respondWithError(w, http.StatusBadRequest, "Missing video file", err)
        return
    }
    defer file.Close()

    contentType := header.Header.Get("Content-Type")
    log.Printf("Header Content-Type: %s", contentType)

    buffer := make([]byte, 512)
    n, err := file.Read(buffer)
    if err != nil && err != io.EOF {
        respondWithError(w, http.StatusInternalServerError, "Failed to read file for MIME detection", err)
        return
    }
    detectedType := http.DetectContentType(buffer[:n])
    log.Printf("Detected Content-Type: %s", detectedType)

    _, err = file.Seek(0, io.SeekStart)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Failed to seek file", err)
        return
    }

    mediaType := contentType
    if mediaType == "" || mediaType == "application/octet-stream" {
        mediaType = detectedType
    }

    parsedMediaType, _, err := mime.ParseMediaType(mediaType)
    if err != nil {
        respondWithError(w, http.StatusBadRequest, "Invalid media type", err)
        return
    }
    if parsedMediaType != "video/mp4" {
        log.Printf("Rejecting file with media type: %s", parsedMediaType)
        respondWithError(w, http.StatusBadRequest, "Video must be an MP4", nil)
        return
    }

    tempFile, err := os.CreateTemp("", "tubely-upload-*.mp4")
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Couldn't create temp file", err)
        return
    }
    defer os.Remove(tempFile.Name())

    _, err = io.Copy(tempFile, file)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Failed to write video to temp file", err)
        return
    }

    _, err = tempFile.Seek(0, io.SeekStart)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Failed to seek temp file", err)
        return
    }

    processedPath, err := processVideoForFastStart(tempFile.Name())
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Failed to process video for fast start", err)
        return
    }
    defer os.Remove(processedPath)

    processedFile, err := os.Open(processedPath)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Failed to open processed file", err)
        return
    }
    defer processedFile.Close()

    prefix, err := getVideoAspectRatio(processedPath)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Failed to determine aspect ratio", err)
        return
    }
    log.Printf("Aspect ratio prefix: %s", prefix)

    randomBytes := make([]byte, 32)
    _, err = rand.Read(randomBytes)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Couldn't generate random key", err)
        return
    }
    fileKeyBase := base64.RawURLEncoding.EncodeToString(randomBytes)
    fileKey := fmt.Sprintf("%s/%s.mp4", prefix, fileKeyBase)

    _, err = cfg.s3Client.PutObject(context.Background(), &s3.PutObjectInput{
        Bucket:      &cfg.s3Bucket,
        Key:         &fileKey,
        Body:        processedFile,
        ContentType: &mediaType,
    })
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Failed to upload video to S3", err)
        return
    }

    // Store the CloudFront URL instead of bucket,key
    videoURL := fmt.Sprintf("https://%s/%s", cfg.cloudFrontDomain, fileKey)
    video.VideoURL = &videoURL
    err = cfg.db.UpdateVideo(video)
    if err != nil {
        respondWithError(w, http.StatusInternalServerError, "Couldn't update video", err)
        return
    }

    respondWithJSON(w, http.StatusOK, video) 
}
