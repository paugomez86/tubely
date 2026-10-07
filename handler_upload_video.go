package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
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

	fmt.Println("uploading video", videoID, "by user", userID)

	// Uploading video
	// Getting video metadata from database and checking if user is owner
	videoMeta, err := cfg.db.GetVideo(videoID)
	if videoMeta.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized action", err)
		return
	}

	// Setting max size of request
	http.MaxBytesReader(w, r.Body, 1024*1024*1024)

	// Retrieving file data from form
	fileData, fileHeader, err := r.FormFile("video")
	defer fileData.Close()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error uploading video", err)
		return
	}

	// Checking file headers and filtering type
	mediaType, _, err := mime.ParseMediaType(fileHeader.Header.Get("Content-Type"))
	allowedTypes := []string{
		"video/mp4",
	}
	if err != nil || !slices.Contains(allowedTypes, mediaType) {
		respondWithError(w, http.StatusBadRequest, "Video type not allowed", err)
		return
	}

	// Creating local temp file to store data
	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error uploading video", err)
		return
	}
	defer os.Remove("/tmp/tubely-upload.mp4")
	defer tempFile.Close()

	// Copying data to temp file
	io.Copy(tempFile, fileData)
	tempFile.Seek(0, io.SeekStart)

	// Building params to upload file to s3
	// File name
	key := make([]byte, 32)
	rand.Read(key)
	fileName := base64.RawURLEncoding.EncodeToString(key)
	// File extension
	mediaTypeSplit := strings.Split(mediaType, "/")
	fileExtension := mediaTypeSplit[1]
	// Adding extension to file name
	fileName = fmt.Sprintf("%v.%v", fileName, fileExtension)

	params := s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileName,
		Body:        tempFile,
		ContentType: &mediaType,
	}

	cfg.s3Client.PutObject(r.Context(), &params)

	// Updating video metadata
	fullVideoURL := fmt.Sprintf("https://%v.s3.%v.amazonaws.com/%v", cfg.s3Bucket, cfg.s3Region, fileName)
	videoMeta.VideoURL = &fullVideoURL

	// Querying database to update video metadata
	err = cfg.db.UpdateVideo(videoMeta)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error uploading video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, videoMeta)
}
