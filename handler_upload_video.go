package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
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
	tempFile, err := os.CreateTemp("", "tubely-upload-*.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error uploading video", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	// Copying data to temp file
	io.Copy(tempFile, fileData)
	tempFile.Seek(0, io.SeekStart)

	// Getting video format from aspect ratio (landscape, portrait or other)
	videoFormat, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		fmt.Println(err)
	}

	// Building params to upload file to s3
	// File name
	key := make([]byte, 32)
	rand.Read(key)
	fileName := base64.RawURLEncoding.EncodeToString(key)
	// File extension
	mediaTypeSplit := strings.Split(mediaType, "/")
	fileExtension := mediaTypeSplit[1]
	// Adding extension to file name
	fileName = fmt.Sprintf("%v/%v.%v", videoFormat, fileName, fileExtension)

	params := s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileName,
		Body:        tempFile,
		ContentType: &mediaType,
	}

	// Uploading file to s3
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

// Gets the file path of the temp file where the video is stored before uploading to S3.
// Runs ffprobe to get the video width and height and checks if it has valid portrait or landscape format dimensions.
func getVideoAspectRatio(filePath string) (string, error) {
	var buffer bytes.Buffer
	var cmd *exec.Cmd

	type dimensions struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	// Running ffprobe
	cmd = exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	cmd.Stdout = &buffer
	cmd.Run()

	// Unmarshaling ffprobe json output
	d := dimensions{}

	err := json.Unmarshal(buffer.Bytes(), &d)
	if err != nil {
		return "", err
	}

	width := d.Streams[0].Width
	height := d.Streams[0].Height

	// Calculating aspect ratio
	var aspectRatio float64
	var videoFormat string

	aspectRatio = float64(width) / float64(height)
	tolerance := 0.1
	landscapeTarget := 1.778
	portraitTarget := 0.5625

	if math.Abs(aspectRatio-landscapeTarget) < tolerance {
		videoFormat = "landscape"
	} else if math.Abs(aspectRatio-portraitTarget) < tolerance {
		videoFormat = "portrait"
	} else {
		videoFormat = "other"
	}

	return videoFormat, nil
}
