package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"

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

	// TODO: implement the upload here
	const maxMemory = 10 << 20 //10 megabytes
	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error parsing thumbnail", err)
		return
	}
	thumbnailFile, thumbnailHddr, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error getting thumbnail file", err)
		return
	}
	defer thumbnailFile.Close()
	mediaType := thumbnailHddr.Header.Get("Content-Type")
	if mediaType == "" {
		respondWithError(w, http.StatusBadRequest, "error getting media type, string is empty", fmt.Errorf("Content-Type key empty or does not exist"))
		return
	}
	mediaTypeParsed, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error parsing mediaType", err)
		return
	}
	if mediaTypeParsed != "image/jpeg" && mediaTypeParsed != "image/png" {
		respondWithError(w, http.StatusForbidden, "file must be jpg or png", fmt.Errorf("incorrect filetype"))
		return
	}

	/*
		imgData, err := io.ReadAll(thumbnailFile)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, "error reading imgData", err)
			return
		}*/
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Couldn't get video", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "This is not you video! Bad client!", err)
		return
	}
	//thumbyStruct := thumbnail{data: imgData, mediaType: mediaType}
	//videoThumbnails[videoID] = thumbyStruct
	//url := fmt.Sprintf("http://localhost:%d/api/thumbnails/%s", 8091, videoID)
	//video.ThumbnailURL = &url
	//err = cfg.db.UpdateVideo(video)
	//if err != nil {
	//	respondWithError(w, http.StatusInternalServerError, "error updating video in db", err)
	//	return
	//}
	//imgDataString := base64.StdEncoding.EncodeToString(imgData)
	//if imgDataString == "" {
	//	respondWithError(w, http.StatusInternalServerError, "error stringifying the image", fmt.Errorf("string is empty"))
	//	return
	//}
	exts, err := mime.ExtensionsByType(mediaType)
	if err != nil || len(exts) == 0 {
		respondWithError(w, http.StatusBadRequest, "unknown media type", err)
		return
	}
	ext := exts[0] // e.g. ".png"
	filenameBytes := make([]byte, 32)
	rand.Read(filenameBytes)
	filename := base64.RawURLEncoding.EncodeToString(filenameBytes)
	filenamePlusExt := fmt.Sprintf("%s%s", filename, ext)
	dataFilePath := filepath.Join("/home/Adam/working/github.com/adam0x59/learn-file-storage-s3-golang-starter/assets", filenamePlusExt)
	file, err := os.Create(dataFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error creating new file", err)
		return
	}
	defer file.Close()
	_, err = io.Copy(file, thumbnailFile)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error copying file to destination", err)
		return
	}
	url := fmt.Sprintf("http://localhost:8091/assets/%s", filenamePlusExt)
	video.ThumbnailURL = &url
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error updating video in db", err)
		return
	}
	// Storing image in sql DB as a bas64 encoded string
	/*dataUrl := fmt.Sprintf("data:%s;base64,%s", mediaType, imgDataString)
	video.ThumbnailURL = &dataUrl
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error updating video in db", err)
		return
	}*/

	respondWithJSON(w, http.StatusOK, video)
}
