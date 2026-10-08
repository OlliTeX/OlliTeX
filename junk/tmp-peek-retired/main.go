package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func main() {
	token, _ := os.ReadFile("/tmp/tk3.txt")
	fmt.Printf("tokenfile[len=%d]=%q\n", len(token), string(token))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cli, err := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:1/t/sharelatex?replicaSet=overleaf"))
	_ = cli
	if err == nil {
		fmt.Println("connect ok (unexpected host)")
	}
	fmt.Println("---")
	cli2, err := mongo.Connect(options.Client().ApplyURI(os.Getenv("MPROBE_URI")))
	if err != nil {
		fmt.Println("connect err:", err)
		return
	}
	if err := cli2.Ping(ctx, nil); err != nil {
		fmt.Println("ping err:", err)
		return
	}
	coll := cli2.Database("sharelatex").Collection("tokens")
	var doc bson.M
	e1 := coll.FindOne(ctx, bson.M{"token": string(token)}).Decode(&doc)
	j, _ := json.Marshal(doc)
	fmt.Println("plain find: err=", e1, "doc=", string(j))
	filter := bson.M{
		"use":       "password",
		"token":     string(token),
		"expiresAt": bson.M{"$gt": time.Now().UTC()},
		"peekCount": bson.M{"$not": bson.M{"$gte": 4}},
	}
	var out struct {
		Data      bson.M `bson:"data"`
		PeekCount int    `bson:"peekCount"`
	}
	e2 := coll.FindOneAndUpdate(ctx, filter, bson.M{"$inc": bson.M{"peekCount": 1}}).Decode(&out)
	fmt.Println("peek: err=", e2, "peekCountBefore=", out.PeekCount)
}
