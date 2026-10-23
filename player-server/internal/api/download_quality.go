package api

import "github.com/snonux/player/internal/service"

func qualityDownloadService(compat service.CompatStreamService) service.QualityStreamService {
	quality, _ := compat.(service.QualityStreamService)
	return quality
}
