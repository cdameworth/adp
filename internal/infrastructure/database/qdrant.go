package database

import (
	"context"
	"fmt"

	pb "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// VectorStore defines the interface for vector database operations
type VectorStore interface {
	Search(ctx context.Context, collection string, vector []float32, limit int) ([]Point, error)
	Upsert(ctx context.Context, collection string, points []Point) error
	Close() error
}

// Point represents a data point in the vector store
type Point struct {
	ID      string
	Vector  []float32
	Payload map[string]interface{}
}

// QdrantStore implements VectorStore for Qdrant
type QdrantStore struct {
	conn   *grpc.ClientConn
	client pb.PointsClient
}

// NewQdrantStore creates a new QdrantStore
func NewQdrantStore(host string, port int) (*QdrantStore, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to qdrant: %w", err)
	}

	client := pb.NewPointsClient(conn)

	return &QdrantStore{
		conn:   conn,
		client: client,
	}, nil
}

// Close closes the connection
func (s *QdrantStore) Close() error {
	return s.conn.Close()
}

// Search performs a vector search
func (s *QdrantStore) Search(ctx context.Context, collection string, vector []float32, limit int) ([]Point, error) {
	resp, err := s.client.Search(ctx, &pb.SearchPoints{
		CollectionName: collection,
		Vector:         vector,
		Limit:          uint64(limit),
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
	})
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	var results []Point
	for _, hit := range resp.Result {
		// Convert payload
		payload := make(map[string]interface{})
		for k, v := range hit.Payload {
			payload[k] = v.GetStringValue() // Simplified: assuming string values for now
		}

		results = append(results, Point{
			ID:      hit.Id.GetUuid(),
			Vector:  nil, // Usually not returned in search unless requested
			Payload: payload,
		})
	}

	return results, nil
}

// Upsert inserts or updates points
func (s *QdrantStore) Upsert(ctx context.Context, collection string, points []Point) error {
	var qPoints []*pb.PointStruct
	for _, p := range points {
		// Convert payload to Qdrant format
		qPayload := make(map[string]*pb.Value)
		for k, v := range p.Payload {
			if strVal, ok := v.(string); ok {
				qPayload[k] = &pb.Value{Kind: &pb.Value_StringValue{StringValue: strVal}}
			}
		}

		qPoints = append(qPoints, &pb.PointStruct{
			Id:      &pb.PointId{PointIdOptions: &pb.PointId_Uuid{Uuid: p.ID}},
			Vectors: &pb.Vectors{VectorsOptions: &pb.Vectors_Vector{Vector: &pb.Vector{Data: p.Vector}}},
			Payload: qPayload,
		})
	}

	_, err := s.client.Upsert(ctx, &pb.UpsertPoints{
		CollectionName: collection,
		Points:         qPoints,
	})
	if err != nil {
		return fmt.Errorf("upsert failed: %w", err)
	}

	return nil
}
