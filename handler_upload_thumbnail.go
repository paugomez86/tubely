package main

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// Uploading thumbnail
	// Getting form data using multipart form
	const maxMemory = 10 << 20

	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error uploading thumbnail", err)
		return
	}

	fileData, fileHeader, err := r.FormFile("thumbnail")
	defer fileData.Close()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error uploading thumbnail", err)
		return
	}

	// Reading file headers
	mediaType, _, err := mime.ParseMediaType(fileHeader.Header.Get("Content-Type"))

	// Filtering types
	allowedTypes := []string{
		"image/png",
		"image/jpeg",
	}
	if !slices.Contains(allowedTypes, mediaType) {
		respondWithError(w, http.StatusBadRequest, "Thumbnail type not allowed", err)
		return
	}

	// Getting video metadata from database
	videoMeta, err := cfg.db.GetVideo(videoID)

	// Checking if user is owner
	if videoMeta.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized action", err)
		return
	}

	// Creating file in assets to store the thumbnail data
	mediaTypeSplit := strings.Split(mediaType, "/")
	fileExtension := mediaTypeSplit[1]
	filePath := filepath.Join(cfg.assetsRoot, fmt.Sprintf("%v.%v", videoID, fileExtension))
	file, err := os.Create(filePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error uploading thumbnail", err)
		return
	}
	io.Copy(file, fileData)

	// Building thumbnail URL
	thumbnailURL := fmt.Sprintf("http://localhost:%v/%v", cfg.port, filePath)

	// Updating video metadata to the database
	videoMeta.ThumbnailURL = &thumbnailURL
	err = cfg.db.UpdateVideo(videoMeta)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error updating video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, videoMeta)
}
