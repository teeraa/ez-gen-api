package storage

import "fmt"

func KeyForOutput(userID, jobID, ext string) string {
	return fmt.Sprintf("outputs/%s/%s.%s", userID, jobID, ext)
}

func KeyForThumbnail(userID, jobID, ext string) string {
	return fmt.Sprintf("thumbs/%s/%s.%s", userID, jobID, ext)
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
