package API

import (
	"net/http"
	"github.com/gin-gonic/gin"
)

// setupRouter limpo
func SetupRouter(s *Store) *gin.Engine {
	r := gin.Default()

    // Apenas rotas úteis para debug ou clientes HTTP legados
	r.GET("/status", func(c *gin.Context) {
        s.mu.Lock()
        defer s.mu.Unlock()
		c.JSON(http.StatusOK, gin.H{
            "node_id": s.NodeID,
            "players_online": len(s.players),
            "matches": len(s.matchHistory),
            "queue": len(s.gameQueue),
        })
	})

    // Se você usar HTTP para jogar cartas (além do NATS), mantenha os handlers,
    // mas aponte diretamente para as funções do store, sem forward.
    
	return r
}