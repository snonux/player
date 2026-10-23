package api

import "codeberg.org/snonux/player/internal/service"

func qualityDownloadService(compat service.CompatStreamService) service.QualityStreamService {
	quality, _ := compat.(service.QualityStreamService)
	return quality
}
