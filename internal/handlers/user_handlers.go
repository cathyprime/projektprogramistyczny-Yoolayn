package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"redoot/internal/msgs"
	"redoot/internal/types"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"
)

func NewUser(c *gin.Context, users *mongo.Collection) {
	body := struct {
		User types.User `json:"user"`
	}{}

	err := decodeBody(c, &body)
	if err != nil {
		return
	}

	_, err = mail.ParseAddress(body.User.Email)
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrWrongEmailFormat,
			"email not formated properly",
		))
		return
	}

	usr := body.User
	log.Debug(msgs.DebugStruct, "usr", fmt.Sprintf("%#v", usr))

	if log.GetLevel() == log.DebugLevel {
		debugJSON, _ := json.MarshalIndent(usr, "", "\t")
		log.Debug(msgs.DebugJSON, "usr", string(debugJSON))
	}

	ok := usr.IsTaken()
	if !ok {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrTaken,
			"username is already taken",
		))
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(usr.Password), bcrypt.MinCost)
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrEncryption,
			"password exceeded the length of 72",
		))
		return
	}
	usr.Password = string(hash)

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
	defer cancel()

	result, optionsErr := users.InsertOne(ctx, usr)
	if optionsErr != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrBadOptions,
			"Bad options provided in the InsertOne",
			optionsErr,
		))
		return
	}
	c.JSON(http.StatusCreated, struct {
		Code   int    `json:"code"`
		Status string `json:"status"`
		ID     string `json:"id"`
	}{
		Code:   http.StatusCreated,
		Status: "OK",
		ID:     result.InsertedID.(primitive.ObjectID).String(),
	})
}

func GetUsers(c *gin.Context, usersColl *mongo.Collection) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
	defer cancel()

	cursor, err := usersColl.Find(ctx, bson.M{})
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrInternal,
			"Bad options provided for Find",
			"reason", "bad options provided for GetUsers",
		))
		return
	}

	var users []types.User
	err = cursor.All(ctx, &users)
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrInternal,
			"Failed decoding cursor",
			"GetUsers cursor", err,
		))
		return
	}
	log.Debug(msgs.DebugStruct, "users", fmt.Sprintf("%#v\n", users))
	c.JSON(http.StatusOK, users)
}

func GetUser(c *gin.Context, users *mongo.Collection) {
	objid, err := idFromParams(c)
	if err != nil {
		return
	}

	filter := struct {
		ID primitive.ObjectID `bson:"_id"`
	}{
		ID: objid,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
	defer cancel()

	result := users.FindOne(ctx, filter)

	var user types.User
	err = result.Decode(&user)
	if err == mongo.ErrNoDocuments {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrNotFound,
			"user not found",
			"getuser", err,
		))
		return
	} else if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrInternal,
			"failed parsing documents, skill issue",
			"msg", err,
		))
		return
	}

	log.Debug(msgs.DebugStruct, "user", fmt.Sprintf("%#v\n", user))
	c.JSON(http.StatusOK, user)
}

func UpdateUser(c *gin.Context, users *mongo.Collection) {
	objid, err := idFromParams(c)
	if err != nil {
		return
	}

	var bdy struct {
		User      types.User        `json:"user"`
		Requester types.Credentials `json:"requester"`
	}

	err = decodeBody(c, &bdy)
	if err != nil {
		return
	}

	if err = bdy.Requester.Authorize(); err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrNotAuthorized,
			"user wasn't authorized",
			"error", err,
		))
		return
	}

	var oldUsr types.User
	err = getAndConvert(users, objid, &oldUsr)
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrInternal,
			"user making skill issue",
		))
		return
	}

	if oldUsr.Name != bdy.User.Name {
		if ok := bdy.User.IsTaken(); !ok {
			c.AbortWithStatusJSON(msgs.ReportError(
				msgs.ErrTaken,
				"username is taken",
			))
			return
		}
	}

	usr, err := bdy.Requester.ToUser()
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrInternal,
			"internal error",
		))
		return
	}

	if objid != usr.ID {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrForbidden,
			"action is forbidden!",
			"UpdateUser", "ids aren't equal",
		))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
	defer cancel()

	hash, err := bcrypt.GenerateFromPassword([]byte(bdy.User.Password), bcrypt.MinCost)
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrEncryption,
			"password exceeded the length of 72",
		))
		return
	}
	bdy.User.Password = string(hash)

	update := bson.M{"$set": bdy.User}

	updateResult, err := users.UpdateByID(ctx, objid, update)
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrBadOptions,
			"id taken",
			"UpdateUser", err,
		))
		return
	}

	if updateResult.ModifiedCount == 0 {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrUpdateFailed,
			"failed to find the user",
			"UpdateUser", updateResult,
		))
		return
	}

	c.JSON(http.StatusAccepted, struct {
		Code   int    `json:"code"`
		Status string `json:"status"`
	}{
		Code:   http.StatusAccepted,
		Status: "OK",
	})
}

func DeleteUser(c *gin.Context, users *mongo.Collection) {
	objid, err := idFromParams(c)
	if err != nil {
		return
	}

	body := struct {
		Requester types.Credentials `json:"requester"`
	}{}

	err = decodeBody(c, &body)
	if err != nil {
		return
	}

	if err := body.Requester.Authorize(); err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrNotAuthorized,
			"user not authorized",
		))
		return
	}

	usr, err := body.Requester.ToUser()
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrInternal,
			"internal error",
		))
		return
	}

	if objid != usr.ID {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrForbidden,
			"action forbidden",
			"UpdateUser", usr.ID == objid,
		))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
	defer cancel()

	filter := bson.M{"_id": objid}

	deleteResult, err := users.DeleteOne(ctx, filter)
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrBadOptions,
			"id taken",
			"DeleteUser", err,
		))
		return
	}

	if deleteResult.DeletedCount != 1 {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrNotFound,
			"user failed to delete",
			"DeleteUser", deleteResult.DeletedCount != 1,
		))
		return
	}
	c.JSON(http.StatusOK, struct {
		Code   int    `json:"code"`
		Status string `json:"status"`
	}{
		Code:   http.StatusOK,
		Status: "OK",
	})
}

func SearchUser(c *gin.Context, users *mongo.Collection) {
	var length int
	for _, v := range c.Request.URL.Query() {
		length += len(v)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
	defer cancel()

	var wg sync.WaitGroup
	ch := make(chan findResultUsers, length)

	for k, s := range c.Request.URL.Query() {
		for _, v := range s {
			wg.Add(1)
			log.Debug("searchuser", k, v)
			go findByFieldUsers(ctx, users, k, v, ch, &wg)
		}
	}

	wg.Wait()
	close(ch)

	var values []types.User
	for v := range ch {
		if err := v.err; err != nil {
			log.Debug(msgs.DebugSkippedLoop, "struct", v)
			continue
		}
		log.Debug("appending", "values +", v)
		values = append(values, v.users...)
	}

	if len(values) == 0 {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrNotFound,
			"no users found with provided parameters",
			"values", values,
		))
		return
	}

	c.JSON(http.StatusOK, values)
}

func MostPopularUsers(c *gin.Context, users, posts, comments *mongo.Collection) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
	defer cancel()

	lookupPostsStage := bson.D{
		{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: "posts"},
			{Key: "localField", Value: "_id"},
			{Key: "foreignField", Value: "author"},
			{Key: "as", Value: "posts"},
		}},
	}

	lookupCommentsStage := bson.D{
		{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: "comments"},
			{Key: "localField", Value: "_id"},
			{Key: "foreignField", Value: "author"},
			{Key: "as", Value: "comments"},
		}},
	}

	projectStage := bson.D{
		{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 0},
			{Key: "name", Value: 1},
			{Key: "votes", Value: bson.D{
				{Key: "$add", Value: bson.A{
					bson.D{{Key: "$sum", Value: "$posts.votes"}},
					bson.D{{Key: "$sum", Value: "$comments.votes"}},
				}},
			}},
		}},
	}

	sortStage := bson.D{
		{Key: "$sort", Value: bson.D{
			{Key: "votes", Value: -1},
		}},
	}

	limitStage := bson.D{
		{Key: "$limit", Value: 15},
	}

	pipeline := mongo.Pipeline{lookupPostsStage, lookupCommentsStage, projectStage, sortStage, limitStage}

	cursor, err := users.Aggregate(ctx, pipeline)
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrInternal,
			"internal error reading from cursor",
		))
		return
	}

	var answers []struct {
		Name  string `json:"name"`
		Votes int    `json:"votes"`
	}

	err = cursor.All(ctx, &answers)
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrInternal,
			"internal server error",
		))
		return
	}

	c.AbortWithStatusJSON(http.StatusOK, answers)
}
