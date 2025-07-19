package tracing

import (
	"context"
	"google.golang.org/grpc"
)

// NewConditionalUnaryInterceptor crea un interceptor gRPC che applica un altro interceptor
// solo se il metodo chiamato non è nella lista dei metodi da saltare.
func NewConditionalUnaryInterceptor(interceptorToApply grpc.UnaryServerInterceptor, methodsToSkip ...string) grpc.UnaryServerInterceptor {
	// Creiamo una mappa per una ricerca veloce dei metodi da saltare.
	skipSet := make(map[string]struct{})
	for _, method := range methodsToSkip {
		skipSet[method] = struct{}{}
	}

	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Controlliamo se il metodo completo della chiamata corrente è nella nostra mappa.
		if _, shouldSkip := skipSet[info.FullMethod]; shouldSkip {
			// Se sì, saltiamo l'interceptor e chiamiamo direttamente l'handler finale.
			return handler(ctx, req)
		}
		// Altrimenti, applichiamo l'interceptor che ci è stato passato.
		return interceptorToApply(ctx, req, info, handler)
	}
}
