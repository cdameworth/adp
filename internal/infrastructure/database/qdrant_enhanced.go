package database

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	pb "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Common errors for vector operations
var (
	ErrCollectionNotFound      = errors.New("collection not found")
	ErrCollectionExists        = errors.New("collection already exists")
	ErrVectorDimensionMismatch = errors.New("vector dimension mismatch")
	ErrEmptyVector             = errors.New("empty vector provided")
)

// EnhancedQdrantStore implements an enhanced VectorStore for Qdrant with collection management
type EnhancedQdrantStore struct {
	conn              *grpc.ClientConn
	pointsClient      pb.PointsClient
	collectionsClient pb.CollectionsClient
	config            *QdrantStoreConfig
	mu                sync.RWMutex
	collectionCache   map[string]*CollectionInfo
}

// QdrantStoreConfig contains configuration for the Qdrant store
type QdrantStoreConfig struct {
	Host            string
	Port            int
	APIKey          string
	DefaultDistance DistanceMetric
	Timeout         time.Duration
	RetryAttempts   int
	RetryDelay      time.Duration
}

// DistanceMetric represents the distance metric for vector comparison
type DistanceMetric string

const (
	DistanceCosine    DistanceMetric = "Cosine"
	DistanceEuclid    DistanceMetric = "Euclid"
	DistanceDot       DistanceMetric = "Dot"
	DistanceManhattan DistanceMetric = "Manhattan"
)

// CollectionInfo contains collection metadata
type CollectionInfo struct {
	Name         string
	VectorSize   uint64
	Distance     DistanceMetric
	PointsCount  uint64
	Status       string
	CreatedAt    time.Time
	IndexedCount uint64
}

// DefaultQdrantConfig returns default configuration
func DefaultQdrantConfig() *QdrantStoreConfig {
	return &QdrantStoreConfig{
		Host:            "localhost",
		Port:            6334,
		DefaultDistance: DistanceCosine,
		Timeout:         30 * time.Second,
		RetryAttempts:   3,
		RetryDelay:      100 * time.Millisecond,
	}
}

// NewEnhancedQdrantStore creates a new enhanced Qdrant store
func NewEnhancedQdrantStore(config *QdrantStoreConfig) (*EnhancedQdrantStore, error) {
	if config == nil {
		config = DefaultQdrantConfig()
	}

	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to qdrant: %w", err)
	}

	store := &EnhancedQdrantStore{
		conn:              conn,
		pointsClient:      pb.NewPointsClient(conn),
		collectionsClient: pb.NewCollectionsClient(conn),
		config:            config,
		collectionCache:   make(map[string]*CollectionInfo),
	}

	return store, nil
}

// Close closes the connection
func (s *EnhancedQdrantStore) Close() error {
	return s.conn.Close()
}

// CreateCollection creates a new collection with the specified configuration
func (s *EnhancedQdrantStore) CreateCollection(ctx context.Context, name string, dimension int) error {
	return s.CreateCollectionWithConfig(ctx, name, dimension, s.config.DefaultDistance)
}

// CreateCollectionWithConfig creates a collection with specific distance metric
func (s *EnhancedQdrantStore) CreateCollectionWithConfig(ctx context.Context, name string, dimension int, distance DistanceMetric) error {
	// Check if collection already exists
	exists, err := s.CollectionExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return ErrCollectionExists
	}

	var distanceEnum pb.Distance
	switch distance {
	case DistanceCosine:
		distanceEnum = pb.Distance_Cosine
	case DistanceEuclid:
		distanceEnum = pb.Distance_Euclid
	case DistanceDot:
		distanceEnum = pb.Distance_Dot
	case DistanceManhattan:
		distanceEnum = pb.Distance_Manhattan
	default:
		distanceEnum = pb.Distance_Cosine
	}

	_, err = s.collectionsClient.Create(ctx, &pb.CreateCollection{
		CollectionName: name,
		VectorsConfig: &pb.VectorsConfig{
			Config: &pb.VectorsConfig_Params{
				Params: &pb.VectorParams{
					Size:     uint64(dimension),
					Distance: distanceEnum,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create collection: %w", err)
	}

	// Update cache
	s.mu.Lock()
	s.collectionCache[name] = &CollectionInfo{
		Name:       name,
		VectorSize: uint64(dimension),
		Distance:   distance,
		CreatedAt:  time.Now(),
		Status:     "green",
	}
	s.mu.Unlock()

	return nil
}

// DeleteCollection deletes a collection
func (s *EnhancedQdrantStore) DeleteCollection(ctx context.Context, name string) error {
	_, err := s.collectionsClient.Delete(ctx, &pb.DeleteCollection{
		CollectionName: name,
	})
	if err != nil {
		return fmt.Errorf("failed to delete collection: %w", err)
	}

	// Remove from cache
	s.mu.Lock()
	delete(s.collectionCache, name)
	s.mu.Unlock()

	return nil
}

// CollectionExists checks if a collection exists
func (s *EnhancedQdrantStore) CollectionExists(ctx context.Context, name string) (bool, error) {
	// Check cache first
	s.mu.RLock()
	if _, ok := s.collectionCache[name]; ok {
		s.mu.RUnlock()
		return true, nil
	}
	s.mu.RUnlock()

	// Query Qdrant
	resp, err := s.collectionsClient.List(ctx, &pb.ListCollectionsRequest{})
	if err != nil {
		return false, fmt.Errorf("failed to list collections: %w", err)
	}

	for _, collection := range resp.Collections {
		if collection.Name == name {
			return true, nil
		}
	}

	return false, nil
}

// GetCollectionInfo retrieves detailed information about a collection
func (s *EnhancedQdrantStore) GetCollectionInfo(ctx context.Context, name string) (*CollectionInfo, error) {
	resp, err := s.collectionsClient.Get(ctx, &pb.GetCollectionInfoRequest{
		CollectionName: name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get collection info: %w", err)
	}

	result := resp.GetResult()
	if result == nil {
		return nil, ErrCollectionNotFound
	}

	var distance DistanceMetric
	vectorParams := result.GetConfig().GetParams().GetVectorsConfig().GetParams()
	if vectorParams != nil {
		switch vectorParams.GetDistance() {
		case pb.Distance_Cosine:
			distance = DistanceCosine
		case pb.Distance_Euclid:
			distance = DistanceEuclid
		case pb.Distance_Dot:
			distance = DistanceDot
		case pb.Distance_Manhattan:
			distance = DistanceManhattan
		}
	}

	info := &CollectionInfo{
		Name:         name,
		VectorSize:   vectorParams.GetSize(),
		Distance:     distance,
		PointsCount:  result.GetPointsCount(),
		IndexedCount: result.GetIndexedVectorsCount(),
		Status:       result.GetStatus().String(),
	}

	// Update cache
	s.mu.Lock()
	s.collectionCache[name] = info
	s.mu.Unlock()

	return info, nil
}

// ListCollections returns all collections
func (s *EnhancedQdrantStore) ListCollections(ctx context.Context) ([]string, error) {
	resp, err := s.collectionsClient.List(ctx, &pb.ListCollectionsRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to list collections: %w", err)
	}

	names := make([]string, len(resp.Collections))
	for i, c := range resp.Collections {
		names[i] = c.Name
	}

	return names, nil
}

// SearchResult for enhanced search
type EnhancedSearchResult struct {
	ID      string
	Score   float64
	Payload map[string]interface{}
	Vector  []float32
}

// Search performs a vector search in a collection
func (s *EnhancedQdrantStore) Search(ctx context.Context, collection string, vector []float32, limit int) ([]Point, error) {
	if len(vector) == 0 {
		return nil, ErrEmptyVector
	}

	resp, err := s.pointsClient.Search(ctx, &pb.SearchPoints{
		CollectionName: collection,
		Vector:         vector,
		Limit:          uint64(limit),
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
	})
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	results := make([]Point, 0, len(resp.Result))
	for _, hit := range resp.Result {
		payload := convertPayload(hit.Payload)
		results = append(results, Point{
			ID:      hit.Id.GetUuid(),
			Payload: payload,
		})
	}

	return results, nil
}

// SearchWithFilter performs a filtered vector search
func (s *EnhancedQdrantStore) SearchWithFilter(ctx context.Context, collection string, vector []float32, limit int, filter map[string]interface{}) ([]EnhancedSearchResult, error) {
	if len(vector) == 0 {
		return nil, ErrEmptyVector
	}

	searchRequest := &pb.SearchPoints{
		CollectionName: collection,
		Vector:         vector,
		Limit:          uint64(limit),
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
		WithVectors:    &pb.WithVectorsSelector{SelectorOptions: &pb.WithVectorsSelector_Enable{Enable: false}},
	}

	// Build filter conditions
	if len(filter) > 0 {
		searchRequest.Filter = buildQdrantFilter(filter)
	}

	resp, err := s.pointsClient.Search(ctx, searchRequest)
	if err != nil {
		return nil, fmt.Errorf("search with filter failed: %w", err)
	}

	results := make([]EnhancedSearchResult, 0, len(resp.Result))
	for _, hit := range resp.Result {
		results = append(results, EnhancedSearchResult{
			ID:      hit.Id.GetUuid(),
			Score:   float64(hit.Score),
			Payload: convertPayload(hit.Payload),
		})
	}

	return results, nil
}

// SearchBatch performs multiple searches in parallel
func (s *EnhancedQdrantStore) SearchBatch(ctx context.Context, collection string, vectors [][]float32, limit int) ([][]EnhancedSearchResult, error) {
	if len(vectors) == 0 {
		return nil, nil
	}

	searches := make([]*pb.SearchPoints, len(vectors))
	for i, vector := range vectors {
		searches[i] = &pb.SearchPoints{
			CollectionName: collection,
			Vector:         vector,
			Limit:          uint64(limit),
			WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
		}
	}

	resp, err := s.pointsClient.SearchBatch(ctx, &pb.SearchBatchPoints{
		CollectionName: collection,
		SearchPoints:   searches,
	})
	if err != nil {
		return nil, fmt.Errorf("batch search failed: %w", err)
	}

	allResults := make([][]EnhancedSearchResult, len(resp.Result))
	for i, batchResult := range resp.Result {
		results := make([]EnhancedSearchResult, 0, len(batchResult.Result))
		for _, hit := range batchResult.Result {
			results = append(results, EnhancedSearchResult{
				ID:      hit.Id.GetUuid(),
				Score:   float64(hit.Score),
				Payload: convertPayload(hit.Payload),
			})
		}
		allResults[i] = results
	}

	return allResults, nil
}

// Upsert inserts or updates points in a collection
func (s *EnhancedQdrantStore) Upsert(ctx context.Context, collection string, points []Point) error {
	if len(points) == 0 {
		return nil
	}

	qPoints := make([]*pb.PointStruct, len(points))
	for i, p := range points {
		qPayload := make(map[string]*pb.Value)
		for k, v := range p.Payload {
			qPayload[k] = convertToQdrantValue(v)
		}

		qPoints[i] = &pb.PointStruct{
			Id:      &pb.PointId{PointIdOptions: &pb.PointId_Uuid{Uuid: p.ID}},
			Vectors: &pb.Vectors{VectorsOptions: &pb.Vectors_Vector{Vector: &pb.Vector{Data: p.Vector}}},
			Payload: qPayload,
		}
	}

	_, err := s.pointsClient.Upsert(ctx, &pb.UpsertPoints{
		CollectionName: collection,
		Points:         qPoints,
	})
	if err != nil {
		return fmt.Errorf("upsert failed: %w", err)
	}

	return nil
}

// UpsertBatch performs a batch upsert with progress tracking
func (s *EnhancedQdrantStore) UpsertBatch(ctx context.Context, collection string, points []Point, batchSize int) error {
	if len(points) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 100
	}

	for i := 0; i < len(points); i += batchSize {
		end := i + batchSize
		if end > len(points) {
			end = len(points)
		}
		batch := points[i:end]
		if err := s.Upsert(ctx, collection, batch); err != nil {
			return fmt.Errorf("batch upsert failed at offset %d: %w", i, err)
		}
	}

	return nil
}

// Delete removes points from a collection
func (s *EnhancedQdrantStore) Delete(ctx context.Context, collection string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	pointIds := make([]*pb.PointId, len(ids))
	for i, id := range ids {
		pointIds[i] = &pb.PointId{PointIdOptions: &pb.PointId_Uuid{Uuid: id}}
	}

	_, err := s.pointsClient.Delete(ctx, &pb.DeletePoints{
		CollectionName: collection,
		Points: &pb.PointsSelector{
			PointsSelectorOneOf: &pb.PointsSelector_Points{
				Points: &pb.PointsIdsList{Ids: pointIds},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}

	return nil
}

// DeleteByFilter removes points matching a filter
func (s *EnhancedQdrantStore) DeleteByFilter(ctx context.Context, collection string, filter map[string]interface{}) error {
	if len(filter) == 0 {
		return errors.New("filter is required for delete by filter")
	}

	_, err := s.pointsClient.Delete(ctx, &pb.DeletePoints{
		CollectionName: collection,
		Points: &pb.PointsSelector{
			PointsSelectorOneOf: &pb.PointsSelector_Filter{
				Filter: buildQdrantFilter(filter),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("delete by filter failed: %w", err)
	}

	return nil
}

// GetPoint retrieves a specific point by ID
func (s *EnhancedQdrantStore) GetPoint(ctx context.Context, collection string, id string) (*Point, error) {
	resp, err := s.pointsClient.Get(ctx, &pb.GetPoints{
		CollectionName: collection,
		Ids:            []*pb.PointId{{PointIdOptions: &pb.PointId_Uuid{Uuid: id}}},
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
		WithVectors:    &pb.WithVectorsSelector{SelectorOptions: &pb.WithVectorsSelector_Enable{Enable: true}},
	})
	if err != nil {
		return nil, fmt.Errorf("get point failed: %w", err)
	}

	if len(resp.Result) == 0 {
		return nil, ErrNotFound
	}

	point := resp.Result[0]
	vector := point.Vectors.GetVector().GetData()

	return &Point{
		ID:      point.Id.GetUuid(),
		Vector:  vector,
		Payload: convertPayload(point.Payload),
	}, nil
}

// CountPoints returns the number of points in a collection
func (s *EnhancedQdrantStore) CountPoints(ctx context.Context, collection string, filter map[string]interface{}) (uint64, error) {
	exact := true
	countRequest := &pb.CountPoints{
		CollectionName: collection,
		Exact:          &exact,
	}

	if len(filter) > 0 {
		countRequest.Filter = buildQdrantFilter(filter)
	}

	resp, err := s.pointsClient.Count(ctx, countRequest)
	if err != nil {
		return 0, fmt.Errorf("count failed: %w", err)
	}

	return resp.Result.Count, nil
}

// CreatePayloadIndex creates an index on a payload field
func (s *EnhancedQdrantStore) CreatePayloadIndex(ctx context.Context, collection, field string, fieldType string) error {
	var schemaType pb.FieldType
	switch fieldType {
	case "keyword":
		schemaType = pb.FieldType_FieldTypeKeyword
	case "integer":
		schemaType = pb.FieldType_FieldTypeInteger
	case "float":
		schemaType = pb.FieldType_FieldTypeFloat
	case "bool":
		schemaType = pb.FieldType_FieldTypeBool
	case "text":
		schemaType = pb.FieldType_FieldTypeText
	default:
		schemaType = pb.FieldType_FieldTypeKeyword
	}

	_, err := s.pointsClient.CreateFieldIndex(ctx, &pb.CreateFieldIndexCollection{
		CollectionName: collection,
		FieldName:      field,
		FieldType:      &schemaType,
	})
	if err != nil {
		return fmt.Errorf("create index failed: %w", err)
	}

	return nil
}

// HealthCheck checks the health of the Qdrant connection
func (s *EnhancedQdrantStore) HealthCheck(ctx context.Context) error {
	_, err := s.collectionsClient.List(ctx, &pb.ListCollectionsRequest{})
	if err != nil {
		return fmt.Errorf("qdrant health check failed: %w", err)
	}
	return nil
}

// Helper functions

func convertPayload(payload map[string]*pb.Value) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range payload {
		result[k] = convertFromQdrantValue(v)
	}
	return result
}

func convertFromQdrantValue(v *pb.Value) interface{} {
	if v == nil {
		return nil
	}
	switch k := v.Kind.(type) {
	case *pb.Value_StringValue:
		return k.StringValue
	case *pb.Value_IntegerValue:
		return k.IntegerValue
	case *pb.Value_DoubleValue:
		return k.DoubleValue
	case *pb.Value_BoolValue:
		return k.BoolValue
	case *pb.Value_ListValue:
		list := make([]interface{}, len(k.ListValue.Values))
		for i, val := range k.ListValue.Values {
			list[i] = convertFromQdrantValue(val)
		}
		return list
	case *pb.Value_StructValue:
		m := make(map[string]interface{})
		for key, val := range k.StructValue.Fields {
			m[key] = convertFromQdrantValue(val)
		}
		return m
	default:
		return nil
	}
}

func convertToQdrantValue(v interface{}) *pb.Value {
	if v == nil {
		return &pb.Value{Kind: &pb.Value_NullValue{}}
	}
	switch val := v.(type) {
	case string:
		return &pb.Value{Kind: &pb.Value_StringValue{StringValue: val}}
	case int:
		return &pb.Value{Kind: &pb.Value_IntegerValue{IntegerValue: int64(val)}}
	case int64:
		return &pb.Value{Kind: &pb.Value_IntegerValue{IntegerValue: val}}
	case float64:
		return &pb.Value{Kind: &pb.Value_DoubleValue{DoubleValue: val}}
	case float32:
		return &pb.Value{Kind: &pb.Value_DoubleValue{DoubleValue: float64(val)}}
	case bool:
		return &pb.Value{Kind: &pb.Value_BoolValue{BoolValue: val}}
	case []interface{}:
		values := make([]*pb.Value, len(val))
		for i, item := range val {
			values[i] = convertToQdrantValue(item)
		}
		return &pb.Value{Kind: &pb.Value_ListValue{ListValue: &pb.ListValue{Values: values}}}
	case []string:
		values := make([]*pb.Value, len(val))
		for i, item := range val {
			values[i] = convertToQdrantValue(item)
		}
		return &pb.Value{Kind: &pb.Value_ListValue{ListValue: &pb.ListValue{Values: values}}}
	case map[string]interface{}:
		fields := make(map[string]*pb.Value)
		for k, v := range val {
			fields[k] = convertToQdrantValue(v)
		}
		return &pb.Value{Kind: &pb.Value_StructValue{StructValue: &pb.Struct{Fields: fields}}}
	default:
		return &pb.Value{Kind: &pb.Value_StringValue{StringValue: fmt.Sprintf("%v", val)}}
	}
}

func buildQdrantFilter(filter map[string]interface{}) *pb.Filter {
	conditions := make([]*pb.Condition, 0)

	for key, value := range filter {
		switch v := value.(type) {
		case string:
			conditions = append(conditions, &pb.Condition{
				ConditionOneOf: &pb.Condition_Field{
					Field: &pb.FieldCondition{
						Key: key,
						Match: &pb.Match{
							MatchValue: &pb.Match_Keyword{Keyword: v},
						},
					},
				},
			})
		case []string:
			// Match any of the values
			anyConditions := make([]*pb.Condition, len(v))
			for i, val := range v {
				anyConditions[i] = &pb.Condition{
					ConditionOneOf: &pb.Condition_Field{
						Field: &pb.FieldCondition{
							Key: key,
							Match: &pb.Match{
								MatchValue: &pb.Match_Keyword{Keyword: val},
							},
						},
					},
				}
			}
			conditions = append(conditions, &pb.Condition{
				ConditionOneOf: &pb.Condition_Filter{
					Filter: &pb.Filter{Should: anyConditions},
				},
			})
		case int, int64:
			var intVal int64
			switch val := v.(type) {
			case int:
				intVal = int64(val)
			case int64:
				intVal = val
			}
			conditions = append(conditions, &pb.Condition{
				ConditionOneOf: &pb.Condition_Field{
					Field: &pb.FieldCondition{
						Key: key,
						Match: &pb.Match{
							MatchValue: &pb.Match_Integer{Integer: intVal},
						},
					},
				},
			})
		case bool:
			conditions = append(conditions, &pb.Condition{
				ConditionOneOf: &pb.Condition_Field{
					Field: &pb.FieldCondition{
						Key: key,
						Match: &pb.Match{
							MatchValue: &pb.Match_Boolean{Boolean: v},
						},
					},
				},
			})
		}
	}

	return &pb.Filter{Must: conditions}
}
