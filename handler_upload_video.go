package main

import (
	"bytes"
	"context"
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
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
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

	// Processing video for fast start
	processedFilePath, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error uploading video", err)
		return
	}
	processedFile, err := os.Open(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error uploading video", err)
		return
	}
	defer os.Remove(processedFilePath)
	defer processedFile.Close()

	// Building params to upload file to s3
	// File name
	key := make([]byte, 32)
	rand.Read(key)
	fileNameBase := base64.RawURLEncoding.EncodeToString(key)
	// File extension
	mediaTypeSplit := strings.Split(mediaType, "/")
	fileExtension := mediaTypeSplit[1]
	// Building s3 key from file video format, file name and extension
	s3Key := fmt.Sprintf("%v/%v.%v", videoFormat, fileNameBase, fileExtension)

	params := s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &s3Key,
		Body:        processedFile,
		ContentType: &mediaType,
	}

	// Uploading file to s3
	cfg.s3Client.PutObject(r.Context(), &params)

	// Updating video metadata
	fileDbURL := fmt.Sprintf("%v,%v", cfg.s3Bucket, s3Key)
	/* fullVideoURL := fmt.Sprintf("https://%v.s3.%v.amazonaws.com/%v", cfg.s3Bucket, cfg.s3Region, fileDbURL) */
	videoMeta.VideoURL = &fileDbURL

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

func processVideoForFastStart(filePath string) (string, error) {
	var buffer bytes.Buffer

	newFilePath := filePath + ".processing"
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", newFilePath)
	cmd.Stdout = &buffer

	err := cmd.Run()
	if err != nil {
		return "", nil
	}

	return newFilePath, nil
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	input := s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}

	presignClient := s3.NewPresignClient(s3Client)
	request, err := presignClient.PresignGetObject(context.Background(), &input, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", err
	}

	return request.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, nil
	}
	videoURLSplit := strings.Split(*video.VideoURL, ",")
	bucket := videoURLSplit[0]
	key := videoURLSplit[1]

	fmt.Printf("Bucket: %v\nKey: %v\n", bucket, key)
	fmt.Printf("Generating presigned URL for video %v\n", video.ID)
	signedURL, err := generatePresignedURL(cfg.s3Client, bucket, key, time.Minute*5)
	if err != nil {
		return video, nil
	}

	video.VideoURL = &signedURL

	return video, nil
}
