package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "os"
    "path/filepath" 
    "strings"

    "github.com/aws/aws-sdk-go-v2/config"       
    "github.com/aws/aws-sdk-go-v2/service/s3"   
    "github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
    "github.com/dgrijalva/jwt-go"
    "github.com/go-chi/chi/v5"
    "github.com/joho/godotenv"
)

type apiConfig struct {
    db         *database.Client
    jwtSecret  string
    port       string
    assetsRoot string
    platform   string
    s3Bucket   string
    s3Region   string
    s3Client   *s3.Client
    cloudFrontDomain string
}

func main() {
    err := godotenv.Load()
    if err != nil {
        log.Fatal("Error loading .env file")
    }

    dbPath := os.Getenv("DB_PATH")
    jwtSecret := os.Getenv("JWT_SECRET")
    port := os.Getenv("PORT")
    assetsRoot := os.Getenv("ASSETS_ROOT")
    platform := os.Getenv("PLATFORM")
    s3Bucket := os.Getenv("S3_BUCKET")
    s3Region := os.Getenv("S3_REGION")
    cloudFrontDomain := os.Getenv("CLOUDFRONT_DOMAIN")
      if cloudFrontDomain == "" {
    log.Fatal("CLOUDFRONT_DOMAIN not set in .env")
    }

    // Load AWS SDK configuration
    cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(s3Region))
    if err != nil {
        log.Fatal("Error loading AWS config:", err)
    }

    // Create S3 client
    s3Client := s3.NewFromConfig(cfg)

    client, err := database.NewClient(dbPath)
    if err != nil {
        log.Fatal("Error connecting to database:", err)
    }
    apiCfg := apiConfig{
        db:         &client,
        jwtSecret:  jwtSecret,
        port:       port,
        assetsRoot: assetsRoot,
        platform:   platform,
        s3Bucket:   s3Bucket,
        s3Region:   s3Region,
        s3Client:   s3Client,
        cloudFrontDomain: cloudFrontDomain,
    }

    r := chi.NewRouter()

    // Public routes (no auth middleware)
    r.Post("/api/login", apiCfg.handlerLogin)
    r.Get("/app/*", apiCfg.assetsHandler)
    r.With(noCacheMiddleware).Get("/assets/*", apiCfg.assetsHandler)

    // Protected routes (with auth middleware)
    r.Group(func(r chi.Router) {
        r.Use(apiCfg.authMiddleware)
        r.Get("/api/videos", apiCfg.handlerVideosRetrieve)
        r.Get("/api/videos/{videoID}", apiCfg.handlerVideoGet)
        r.Post("/api/videos", apiCfg.handlerVideoMetaCreate)
        r.Post("/api/thumbnail_upload/{videoID}", apiCfg.handlerUploadThumbnail)
        r.Post("/api/video_upload/{videoID}", apiCfg.handlerUploadVideo)
        r.Delete("/api/videos/{videoID}", apiCfg.handlerVideoMetaDelete) 
    })

    fmt.Printf("Server starting on port %s...\n", port)
    err = http.ListenAndServe(":"+port, r)
    if err != nil {
        log.Fatal("Server failed to start:", err)
    }
}

func (cfg *apiConfig) authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        authHeader := r.Header.Get("Authorization")
        if authHeader == "" {
            respondWithError(w, http.StatusUnauthorized, "Missing Authorization header", nil)
            return
        }

        tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
        if tokenStr == authHeader {
            respondWithError(w, http.StatusUnauthorized, "Invalid Authorization header format", nil)
            return
        }

        token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
            if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
                return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
            }
            return []byte(cfg.jwtSecret), nil
        })
        if err != nil {
            respondWithError(w, http.StatusUnauthorized, "Invalid token: "+err.Error(), nil)
            return
        }

        if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
            userID, ok := claims["sub"].(string)
            if !ok {
                respondWithError(w, http.StatusUnauthorized, "Invalid token claims: missing sub", nil)
                return
            }
            ctx := context.WithValue(r.Context(), "userID", userID)
            next.ServeHTTP(w, r.WithContext(ctx))
        } else {
            respondWithError(w, http.StatusUnauthorized, "Invalid token claims: claims invalid or token not valid", nil)
            return
        }
    })
}


func (cfg *apiConfig) assetsHandler(w http.ResponseWriter, r *http.Request) {
    path := r.URL.Path
    log.Printf("Requested path: %s", path)
    if strings.HasPrefix(path, "/app") {
        if path == "/app" || path == "/app/" {
            filePath := filepath.Join(cfg.assetsRoot, "index.html")
            log.Printf("Serving UI file: %s", filePath)
            http.ServeFile(w, r, filePath)
            return
        }
        log.Printf("Serving app dir: %s", cfg.assetsRoot)
        http.StripPrefix("/app/", http.FileServer(http.Dir(cfg.assetsRoot))).ServeHTTP(w, r)
    } else if strings.HasPrefix(path, "/assets") {
        assetsDir := "assets/assets"
        log.Printf("Serving assets dir: %s", assetsDir)
        http.StripPrefix("/assets/", http.FileServer(http.Dir(assetsDir))).ServeHTTP(w, r)
    }
}
