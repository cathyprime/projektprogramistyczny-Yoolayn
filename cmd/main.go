package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/UniversityOfGdanskProjects/projektprogramistyczny-Yoolayn/internal/handlers"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	mongoUri = "mongodb://localhost:27017"
	auth     = options.Credential{
		Username: "root",
		Password: "example",
	}
)

type connection struct {
	con *mongo.Client
	err error
}

func setupMongo(ch chan<- connection) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoUri).SetAuth(auth))
	ch <- connection{
		con: client,
		err: err,
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
	defer cancel()

	ch := make(chan connection)
	defer close(ch)
	go setupMongo(ch)

	var client *mongo.Client
	connectionResult := <-ch
	if connectionResult.err != nil {
		log.Fatal(connectionResult.err)
	}

	client = connectionResult.con
	defer func() {
		err := client.Disconnect(ctx)
		if err != nil {
			panic(err)
		}
	}()

	db := client.Database("redoot")
	users := db.Collection("users")

	r := gin.Default()
	r.GET("/", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
		defer cancel()
		filter := bson.M{"_id": handlers.CreateHelloWorld(ctx, users).InsertedID}
		var resultFind struct {
			Message string `bson:"message"`
		}

		err := users.FindOne(ctx, filter).Decode(&resultFind)
		if err != nil {
			log.Fatal(err)
		}

		c.String(http.StatusOK, resultFind.Message)
	})

	r.POST("/posts", func(c *gin.Context) { handlers.NewPost(c, users) })
	r.GET("/users", func(c *gin.Context) { handlers.GetUsers(c, users) })
	r.POST("/users", func(c *gin.Context) { handlers.NewUser(c, users) })

	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutdown Server...")

	quitCtx, quitCancel := context.WithTimeout(context.Background(), time.Second*5)
	defer quitCancel()
	if err := srv.Shutdown(quitCtx); err != nil {
		log.Fatal("Error Shutting down: ", err)
	}

	err := users.Drop(quitCtx)
	if err != nil {
		log.Fatal("Failed to drop users ", err)
	}
}
