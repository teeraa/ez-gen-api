package storage

import "fmt"

func KeyForOutput(userID, jobID, ext string) string {
	return fmt.Sprintf("outputs/%s/%s.%s", userID, jobID, ext)
}

func KeyForThumbnail(userID, jobID, ext string) string {
	return fmt.Sprintf("thumbs/%s/%s.%s", userID, jobID, ext)
}

func KeyForExport(userID, exportID, ext string) string {
	return fmt.Sprintf("exports/%s/%s.%s", userID, exportID, ext)
}

func KeyForUpload(userID, uploadID, ext string) string {
	return fmt.Sprintf("uploads/%s/%s.%s", userID, uploadID, ext)
}

func KeyForAudioUpload(userID, uploadID, ext string) string {
	return fmt.Sprintf("audio/%s/%s.%s", userID, uploadID, ext)
}

// ExtForContentType falls back to the given default so an unexpected provider
// content type never blocks persisting the output.
func ExtForContentType(contentType, fallback string) string {
	switch contentType {
	case "video/mp4":
		return "mp4"
	case "video/webm":
		return "webm"
	case "video/quicktime":
		return "mov"
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	case "image/webp":
		return "webp"
	default:
		return fallback
	}
}
