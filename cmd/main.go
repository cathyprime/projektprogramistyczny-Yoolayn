package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/UniversityOfGdanskProjects/projektprogramistyczny-Yoolayn/internal/handlers"
	"github.com/UniversityOfGdanskProjects/projektprogramistyczny-Yoolayn/internal/msgs"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
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
	if err != nil {
		log.Fatal("invalid options: ", err)
	}

	err = client.Ping(ctx, nil)
	ch <- connection{
		con: client,
		err: err,
	}
}

func newStyle() (style *log.Styles) {
	style = log.DefaultStyles()
	pinkText := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ffc0cb"))

	grayText := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#808080"))

	style.Key = pinkText
	style.Value = grayText
	return
}

const (
	LevelsDebug   = "debug"
	LevelsInfo    = "info"
	LevelsWarning = "warn"
	LevelsError   = "error"
	LevelsFatal   = "fatal"
)

func setLevel() {
	switch level := os.Getenv("LOG"); level {
	case LevelsDebug:
		log.SetLevel(log.DebugLevel)
	case LevelsInfo:
		log.SetLevel(log.InfoLevel)
	case LevelsWarning:
		log.SetLevel(log.WarnLevel)
	case LevelsError:
		log.SetLevel(log.ErrorLevel)
	case LevelsFatal:
		log.SetLevel(log.FatalLevel)
	default:
		log.SetLevel(log.InfoLevel)
	}
}

func main() {
	log.SetStyles(newStyle())
	setLevel()
	log.Info("starting")

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)

	ch := make(chan connection)
	defer close(ch)
	go setupMongo(ch)

	var client *mongo.Client
	connectionResult := <-ch
	if connectionResult.err != nil {
		log.Fatal(msgs.ErrTypeConn, "database connection", connectionResult.err)
	}

	client = connectionResult.con
	defer func() {
		err := client.Disconnect(ctx)
		if err != nil {
			log.Fatal(msgs.ErrTypeConn, "database disconnect", err)
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
	r.GET("/users/:id", func(c *gin.Context) { handlers.GetUser(c, users) })
	r.PUT("/users/:id", func(c *gin.Context) { handlers.UpdateUser(c, users) })
	r.DELETE("/users/:id", func(c *gin.Context) { handlers.DeleteUser(c, users) })

	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	go handlers.Interrupt(srv, users)

	cancel()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %s\n", err)
	}
}
