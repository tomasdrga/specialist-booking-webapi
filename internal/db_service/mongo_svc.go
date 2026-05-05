package db_service

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type DbService[DocType interface{}] interface {
	CreateDocument(ctx context.Context, id string, document *DocType) error
	FindDocument(ctx context.Context, id string) (*DocType, error)
	UpdateDocument(ctx context.Context, id string, document *DocType) error
	DeleteDocument(ctx context.Context, id string) error
	Disconnect(ctx context.Context) error
}

var ErrNotFound = fmt.Errorf("document not found")
var ErrConflict = fmt.Errorf("conflict: document already exists")

type MongoServiceConfig struct {
	ServerHost string
	ServerPort int
	UserName   string
	Password   string
	DbName     string
	Collection string
	Timeout    time.Duration
}

type mongoSvc[DocType interface{}] struct {
	MongoServiceConfig
	client     atomic.Pointer[mongo.Client]
	clientLock sync.Mutex
}

func NewMongoService[DocType interface{}](config MongoServiceConfig) DbService[DocType] {
	enviro := func(name string, defaultValue string) string {
		if value, ok := os.LookupEnv(name); ok {
			return value
		}
		return defaultValue
	}

	svc := &mongoSvc[DocType]{}
	svc.MongoServiceConfig = config
	if svc.ServerHost == "" {
		svc.ServerHost = enviro("SPECIALIST_BOOKING_API_MONGODB_HOST", "localhost")
	}
	if svc.ServerPort == 0 {
		port := enviro("SPECIALIST_BOOKING_API_MONGODB_PORT", "27017")
		if parsed, err := strconv.Atoi(port); err == nil {
			svc.ServerPort = parsed
		} else {
			svc.ServerPort = 27017
		}
	}
	if svc.UserName == "" {
		svc.UserName = enviro("SPECIALIST_BOOKING_API_MONGODB_USERNAME", "")
	}
	if svc.Password == "" {
		svc.Password = enviro("SPECIALIST_BOOKING_API_MONGODB_PASSWORD", "")
	}
	if svc.DbName == "" {
		svc.DbName = enviro("SPECIALIST_BOOKING_API_MONGODB_DATABASE", "specialist-booking")
	}
	if svc.Collection == "" {
		svc.Collection = enviro("SPECIALIST_BOOKING_API_MONGODB_COLLECTION", "clinics")
	}
	if svc.Timeout == 0 {
		seconds := enviro("SPECIALIST_BOOKING_API_MONGODB_TIMEOUT_SECONDS", "10")
		if parsed, err := strconv.Atoi(seconds); err == nil {
			svc.Timeout = time.Duration(parsed) * time.Second
		} else {
			svc.Timeout = 10 * time.Second
		}
	}
	log.Printf("MongoDB config: //%v@%v:%v/%v/%v", svc.UserName, svc.ServerHost, svc.ServerPort, svc.DbName, svc.Collection)
	return svc
}

func (m *mongoSvc[DocType]) connect(ctx context.Context) (*mongo.Client, error) {
	client := m.client.Load()
	if client != nil {
		return client, nil
	}
	m.clientLock.Lock()
	defer m.clientLock.Unlock()
	client = m.client.Load()
	if client != nil {
		return client, nil
	}
	ctx, cancel := context.WithTimeout(ctx, m.Timeout)
	defer cancel()
	uri := fmt.Sprintf("mongodb://%v:%v", m.ServerHost, m.ServerPort)
	if len(m.UserName) != 0 {
		uri = fmt.Sprintf("mongodb://%v:%v@%v:%v", m.UserName, m.Password, m.ServerHost, m.ServerPort)
	}
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetConnectTimeout(10*time.Second))
	if err != nil {
		return nil, err
	}
	m.client.Store(client)
	return client, nil
}

func (m *mongoSvc[DocType]) collection(ctx context.Context) (*mongo.Collection, context.Context, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(ctx, m.Timeout)
	client, err := m.connect(ctx)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return client.Database(m.DbName).Collection(m.Collection), ctx, cancel, nil
}

func (m *mongoSvc[DocType]) Disconnect(ctx context.Context) error {
	client := m.client.Load()
	if client == nil {
		return nil
	}
	m.clientLock.Lock()
	defer m.clientLock.Unlock()
	client = m.client.Load()
	defer m.client.Store(nil)
	if client != nil {
		return client.Disconnect(ctx)
	}
	return nil
}

func (m *mongoSvc[DocType]) CreateDocument(ctx context.Context, id string, document *DocType) error {
	collection, ctx, cancel, err := m.collection(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	result := collection.FindOne(ctx, bson.D{{Key: "id", Value: id}})
	switch result.Err() {
	case nil:
		return ErrConflict
	case mongo.ErrNoDocuments:
	default:
		return result.Err()
	}
	_, err = collection.InsertOne(ctx, document)
	return err
}

func (m *mongoSvc[DocType]) FindDocument(ctx context.Context, id string) (*DocType, error) {
	collection, ctx, cancel, err := m.collection(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	result := collection.FindOne(ctx, bson.D{{Key: "id", Value: id}})
	switch result.Err() {
	case nil:
	case mongo.ErrNoDocuments:
		return nil, ErrNotFound
	default:
		return nil, result.Err()
	}
	var document DocType
	if err := result.Decode(&document); err != nil {
		return nil, err
	}
	return &document, nil
}

func (m *mongoSvc[DocType]) UpdateDocument(ctx context.Context, id string, document *DocType) error {
	collection, ctx, cancel, err := m.collection(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	result := collection.FindOneAndReplace(ctx, bson.D{{Key: "id", Value: id}}, document)
	switch result.Err() {
	case nil:
		return nil
	case mongo.ErrNoDocuments:
		return ErrNotFound
	default:
		return result.Err()
	}
}

func (m *mongoSvc[DocType]) DeleteDocument(ctx context.Context, id string) error {
	collection, ctx, cancel, err := m.collection(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	result, err := collection.DeleteOne(ctx, bson.D{{Key: "id", Value: id}})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}
