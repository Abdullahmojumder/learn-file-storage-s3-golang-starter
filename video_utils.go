package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "os/exec"
    
)

type FFProbeOutput struct {
    Streams []struct {
        Width  int `json:"width"`
        Height int `json:"height"`
    } `json:"streams"`
}

func getVideoAspectRatio(filePath string) (string, error) {
    cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
    var out bytes.Buffer
    cmd.Stdout = &out
    err := cmd.Run()
    if err != nil {
        return "", err
    }

    var result FFProbeOutput
    err = json.Unmarshal(out.Bytes(), &result)
    if err != nil {
        return "", err
    }

    if len(result.Streams) == 0 {
        return "", fmt.Errorf("no video streams found")
    }

    width := result.Streams[0].Width
    height := result.Streams[0].Height

    if height == 0 {
        return "other", nil
    }
    aspectRatio := float64(width) / float64(height)

    target169 := 16.0 / 9.0
    tolerance := 0.1

    if aspectRatio >= target169-tolerance && aspectRatio <= target169+tolerance {
        return "landscape", nil
    } else if 1.0/aspectRatio >= target169-tolerance && 1.0/aspectRatio <= target169+tolerance {
        return "portrait", nil
    } else {
        return "other", nil
    }
}

func processVideoForFastStart(filePath string) (string, error) {
    // Create output file path with .processing suffix
    outputPath := filePath + ".processing"

    // Run ffmpeg command for fast start encoding
    cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputPath)
    err := cmd.Run()
    if err != nil {
        return "", fmt.Errorf("failed to process video for fast start: %v", err)
    }

    return outputPath, nil
}
