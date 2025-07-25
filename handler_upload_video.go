package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {

	maxUploadLimit := int64(1 << 30) //Limit set to 1Gb
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadLimit)

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
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Couldn't get video", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "This is not you video! Bad client!", err)
		return
	}
	videoFile, videoHddr, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error getting video file", err)
		return
	}
	defer videoFile.Close()
	mediaType := videoHddr.Header.Get("Content-Type")
	mediaTypeParsed, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error parsing mediaType", err)
		return
	}
	if mediaTypeParsed != "video/mp4" {
		respondWithError(w, http.StatusForbidden, "file must be jpg or png", fmt.Errorf("incorrect filetype"))
		return
	}
	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error creating temporary file", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()
	_, err = io.Copy(tempFile, videoFile)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "errpr copying videoFile to temp file", err)
		return
	}
	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error resetting temp file pointer", err)
		return
	}
	exts, err := mime.ExtensionsByType(mediaType)
	if err != nil || len(exts) == 0 {
		respondWithError(w, http.StatusBadRequest, "unknown media type", err)
		return
	}
	aspectRatio, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error getting aspect ratio as:", err)
	}
	var prefix string
	switch aspectRatio {
	case "16:9":
		prefix = "landscape/"
	case "9:16":
		prefix = "portrait/"
	default:
		prefix = "other/"
	}
	ext := exts[0] // e.g. ".mp4"

	processedFilePath, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error processing video file", err)
	}
	os.Remove(tempFile.Name())
	processedTmpFile, err := os.Open(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error opening processed file", err)
	}

	filenameBytes := make([]byte, 32)
	rand.Read(filenameBytes)
	filename := base64.RawURLEncoding.EncodeToString(filenameBytes)
	filenamePlusExt := fmt.Sprintf("%s%s%s", prefix, filename, ext)
	s3PutObj := s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &filenamePlusExt,
		Body:        processedTmpFile,
		ContentType: &mediaType,
	}
	_, err = cfg.s3Client.PutObject(r.Context(), &s3PutObj)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error uploading file to s3", err)
		return
	}
	//bucketKey := fmt.Sprintf("%s,%s", cfg.s3Bucket, filenamePlusExt)
	url := fmt.Sprintf("https://d1lgsb02jge9rs.cloudfront.net/%s", filenamePlusExt)
	video.VideoURL = &url
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error updating video in db", err)
	}
	//cfg.dbVideoToSignedVideo(video)
}

func getVideoAspectRatio(filepath string) (string, error) {
	var buf []byte
	buff := bytes.NewBuffer(buf)
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filepath)
	cmd.Stdout = buff
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("error running ffprobe command as: %w", err)
	}
	type stream struct {
		W int `json:"width"`
		H int `json:"height"`
	}
	var parsed struct {
		Streams []stream `json:"streams"`
	}

	if err := json.Unmarshal(buff.Bytes(), &parsed); err != nil {
		return "", fmt.Errorf("error unmarshalling ffprobe output as: %w", err)
	}

	for _, s := range parsed.Streams {
		if s.W > 0 && s.H > 0 {
			w := s.W
			h := s.H
			ratio := float64(w) / float64(h)
			switch {
			case ratio > 1.7 && ratio < 1.8:
				return "16:9", nil
			case ratio < 0.6:
				return "9:16", nil
			default:
				return "other", nil
			}
		}
	}
	return "", fmt.Errorf("no valid video stream found")
}

func processVideoForFastStart(filePath string) (string, error) {
	outFilePath := filePath + ".processing"
	var buf []byte
	buff := bytes.NewBuffer(buf)
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outFilePath)
	cmd.Stdout = buff
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("error processing video file for fast start as: %w", err)
	}
	return outFilePath, nil
}

/*
func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	ctx := context.Background()
	preClient := s3.NewPresignClient(s3Client)
	getInput := s3.GetObjectInput{Bucket: &bucket, Key: &key}
	resp, err := preClient.PresignGetObject(ctx, &getInput, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", fmt.Errorf("failed to generate a presigned url: %w", err)
	}
	return resp.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	split := strings.Split(*video.VideoURL, ",")
	if len(split) != 2 {
		return database.Video{}, fmt.Errorf("invalid video URL format: expected 'bucket,key'")
	}
	bucket := split[0]
	key := split[1]
	expireTime := time.Second * 60
	url, err := generatePresignedURL(cfg.s3Client, bucket, key, expireTime)
	if err != nil {
		return database.Video{}, fmt.Errorf("error generating presigned url as: %w", err)
	}
	video.VideoURL = &url
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		return database.Video{}, fmt.Errorf("error updating video in db: %w", err)
	}
	return video, nil
}*/
