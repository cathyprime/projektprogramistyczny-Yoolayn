package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"redoot/internal/msgs"
	"redoot/internal/types"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

func NewBoard(c *gin.Context, boards *mongo.Collection) {
	body := struct {
		Board     types.Board       `json:"board"`
		Requester types.Credentials `json:"requester"`
	}{}

	err := decodeBody(c, &body)
	if err != nil {
		return
	}

	board := body.Board
	log.Debug(msgs.DebugStruct, "board", fmt.Sprintf("%#v", board))

	if log.GetLevel() == log.DebugLevel {
		debugJSON, _ := json.MarshalIndent(board, "", "  ")
		log.Debug(msgs.DebugJSON, "board", string(debugJSON))
	}

	err = body.Requester.Authorize()
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrNotAuthorized,
			"user wasn't authorized",
			"error", err,
		))
		return
	}

	usr, err := body.Requester.ToUser()
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrInternal,
			"skill issue",
			"error", err,
		))
		return
	}

	if body.Board.Owner != usr.ID {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrForbidden,
			"can't create board for someone else",
		))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
	defer cancel()

	result, optionsErr := boards.InsertOne(ctx, board)
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

func GetBoards(c *gin.Context, boardsColl *mongo.Collection) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
	defer cancel()

	cursor, err := boardsColl.Find(ctx, bson.M{})
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrInternal,
			"Bad options provided for Find",
			"reason", "bad options provided for GetBoards",
		))
		return
	}

	var boards []types.Board
	err = cursor.All(ctx, &boards)
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrInternal,
			"Failed decoding cursor",
			"GetBoards cursor", err,
		))
		return
	}
	log.Debug(msgs.DebugStruct, "users", fmt.Sprintf("%#v\n", boards))
	c.JSON(http.StatusOK, boards)
}

func GetBoard(c *gin.Context, boards *mongo.Collection) {
	objid, err := idFromParams(c)
	if err != nil {
		return
	}

	filter := bson.M{"_id": objid}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
	defer cancel()

	result := boards.FindOne(ctx, filter)

	var board types.Board
	err = result.Decode(&board)
	if err == mongo.ErrNoDocuments {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrNotFound,
			"board not found",
			"GetBoard", err,
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

	log.Debug(msgs.DebugStruct, "board", fmt.Sprintf("%#v\n", board))
	c.JSON(http.StatusOK, board)
}

func UpdateBoard(c *gin.Context, boards *mongo.Collection, users *mongo.Collection) {
	objid, err := idFromParams(c)
	if err != nil {
		return
	}

	var bdy struct {
		Board     types.Board       `json:"board"`
		Requester types.Credentials `json:"requester"`
	}
	err = decodeBody(c, &bdy)
	if err != nil {
		return
	}

	var board types.Board
	err = getAndConvert(boards, objid, &board)
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrNotFound,
			"not found",
		))
		return
	}

	if err := bdy.Requester.Authorize(); err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrNotAuthorized,
			"user not authorized",
		))
		return
	}

	user, err := bdy.Requester.ToUser()
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrInternal,
			"failed getting user",
		))
		return
	}

	if !(types.IsAdmin(user) || types.IsModerator(board, user) || board.Owner == user.ID) {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrForbidden,
			"action is forbidden!",
			"UpdateBoard", "is neither an admin, moderator nor owner",
		))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
	defer cancel()

	update := bson.M{"$set": bdy.Board}

	updateResult, err := boards.UpdateByID(ctx, objid, update)
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrBadOptions,
			"options failure",
			"UpdateBoard", err,
		))
		return
	}

	if updateResult.ModifiedCount == 0 {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrUpdateFailed,
			"failed to update the board",
			"UpdateBoard", updateResult,
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

func DeleteBoard(c *gin.Context, boards *mongo.Collection, users *mongo.Collection) {
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

	var board types.Board
	err = getAndConvert(boards, objid, &board)
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrNotFound,
			"board finding skill issue",
		))
		return
	}

	usr, err := body.Requester.ToUser()
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrNotFound,
			"user finding skill issue",
		))
		return
	}

	if !(types.IsAdmin(usr) || types.IsModerator(board, usr) || board.Owner == usr.ID) {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrForbidden,
			"action is forbidden!",
			"DeleteBoard", "is neither an admin, moderator nor owner",
		))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
	defer cancel()

	deleteResult, err := boards.DeleteOne(ctx, bson.M{"_id": objid})
	if err != nil {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrBadOptions,
			"internal error",
			"DeleteBoard", err,
		))
		return
	}

	if deleteResult.DeletedCount != 1 {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrNotFound,
			"board failed to delete",
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

func SearchBoard(c *gin.Context, boards *mongo.Collection) {
	var length int
	for _, v := range c.Request.URL.Query() {
		length += len(v)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*200)
	defer cancel()

	var wg sync.WaitGroup
	ch := make(chan findResultBoards, length)

	for k, s := range c.Request.URL.Query() {
		for _, v := range s {
			wg.Add(1)
			go findByFieldBoards(ctx, boards, k, v, ch, &wg)
		}
	}

	wg.Wait()
	close(ch)

	var values []types.Board
	for v := range ch {
		if err := v.err; err != nil {
			log.Debug(msgs.DebugSkippedLoop, "struct", v)
			continue
		}
		log.Debug("appending", "values +", v)
		values = append(values, v.boards...)
	}

	if len(values) == 0 {
		c.AbortWithStatusJSON(msgs.ReportError(
			msgs.ErrNotFound,
			"no boards found with provided parameters",
			"values", values,
		))
		return
	}

	c.JSON(http.StatusOK, values)
}
