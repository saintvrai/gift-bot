package handler

import (
	"gift-bot/internal/service"
	"gift-bot/pkg/util"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
)

type Handlers struct {
	services *service.Services
}

func NewHandlers(services *service.Services) *Handlers {
	return &Handlers{services: services}

}

func (h *Handlers) InitRoutes() *gin.Engine {
	router := gin.Default()
	pprof.Register(router)
	router.Use(util.CORS())

	router.GET("/ping", func(c *gin.Context) {})

	return router
}
