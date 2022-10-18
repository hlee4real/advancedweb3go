package main

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var collection *mongo.Collection

type APIResponse struct {
	Message string      `json:"message"`
	Code    int         `json:"code"`
	Data    interface{} `json:"data"`
}

type DatabaseRequest struct {
}

type LogResponseCreated struct {
	User      common.Address
	RequestId *big.Int
	PrizeId   []*big.Int
	TxHash    common.Hash
}
type LogRequestCreated struct {
	User      common.Address
	RequestId *big.Int
	Amount    *big.Int
	TxHash    common.Hash
}

func main() {
	mongoclient, err := mongo.Connect(context.TODO(), options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		fmt.Println(err)
		return
	}
	collection = mongoclient.Database("wheelV2").Collection("events")
	client, err := ethclient.Dial("https://bsc-mainnet.nodereal.io/v1/9f0529d15c8c42d998eea5ecfe012662")
	if err != nil {
		fmt.Println(err)
		return
	}
	contractAddress := common.HexToAddress("0x7E2E8B536017584ecC660deF55f235933049392d")
	// number := 22460675
	// number := 22460588
	fromBlock := big.NewInt(20977112)
	toBlock := fromBlock.Sub(fromBlock, big.NewInt(1))
	// const blockDuration :=
	// for {
	// 	fromBlock := fromBlock.Add(toBlock, big.NewInt(1))
	// 	toBlock = toBlock.Add(toBlock, big.NewInt(1000))
	// }
	query := ethereum.FilterQuery{
		FromBlock: fromBlock,
		ToBlock:   toBlock,
		Addresses: []common.Address{
			contractAddress,
		},
	}
	logs, err := client.FilterLogs(context.Background(), query)
	if err != nil {
		fmt.Println(err)
		return
	}
	logRequestCreatedSig := []byte("RequestCreated(address,uint256,uint256)")
	logRequestCreatedSigHash := crypto.Keccak256Hash(logRequestCreatedSig).Hex()
	logResponseCreatedSig := []byte("ResponseCreated(address,uint256,uint256[])")
	logResponseCreatedSigHash := crypto.Keccak256Hash(logResponseCreatedSig).Hex()
	// xxxx this moment
	collection.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "txHash", Value: 1},
			{Key: "index", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})

	for _, vLog := range logs {
		if len(vLog.Topics) > 0 && vLog.Topics[0].Hex() == logRequestCreatedSigHash {
			var requestCreated LogRequestCreated
			requestCreated.User = common.HexToAddress(vLog.Topics[1].Hex())
			requestCreated.RequestId = big.NewInt(0).SetBytes(vLog.Topics[2].Bytes())
			requestCreated.Amount = big.NewInt(0).SetBytes(vLog.Data)
			requestCreated.TxHash = vLog.TxHash
			// fmt.Println("-----REQUEST-----")
			// fmt.Printf("User: %s, Req ID: %d, Amount: %d, TxHash: %s, Index: %d \n", requestCreated.User, requestCreated.RequestId, requestCreated.Amount, requestCreated.TxHash.Hex(), vLog.Index)
			collection.InsertOne(context.Background(), bson.D{
				{Key: "txHash", Value: requestCreated.TxHash.Hex()},
				{Key: "index", Value: vLog.Index},
				{Key: "type", Value: "request"},
				{Key: "requestID", Value: requestCreated.RequestId.Int64()},
				{Key: "user", Value: strings.ToLower(requestCreated.User.Hex())},
				{Key: "amount", Value: requestCreated.Amount.Int64()},
			})
		} else if len(vLog.Topics) > 0 && vLog.Topics[0].Hex() == logResponseCreatedSigHash {
			var responseCreated LogResponseCreated
			responseCreated.User = common.HexToAddress(vLog.Topics[1].Hex())
			responseCreated.RequestId = big.NewInt(0).SetBytes(vLog.Topics[2].Bytes())
			responseCreated.PrizeId = make([]*big.Int, 0)
			responseCreated.TxHash = vLog.TxHash
			//prizeID is like: 0 1 2
			//use loop to get all prizeID
			for i := 0; i < len(vLog.Data); i += 32 {
				responseCreated.PrizeId = append(responseCreated.PrizeId, big.NewInt(0).SetBytes(vLog.Data[i:i+32]))
			}
			if len(responseCreated.PrizeId) > 1 {
				responseCreated.PrizeId = responseCreated.PrizeId[2:]
			}
			//convert prizeid to int64
			prizeIdInt64 := make([]int64, 0)
			for _, v := range responseCreated.PrizeId {
				prizeIdInt64 = append(prizeIdInt64, v.Int64())
			}
			// fmt.Println("-----RESPONSE-----")
			// fmt.Printf("User: %s, Req ID: %d, Prize: %v, TxHash: %s, Index: %d \n", responseCreated.User, responseCreated.RequestId, responseCreated.PrizeId, responseCreated.TxHash.Hex(), vLog.Index)
			collection.InsertOne(context.Background(), bson.D{
				{Key: "txHash", Value: responseCreated.TxHash.Hex()},
				{Key: "index", Value: vLog.Index},
				{Key: "type", Value: "response"},
				{Key: "requestID", Value: responseCreated.RequestId.Int64()},
				{Key: "user", Value: strings.ToLower(responseCreated.User.Hex())},
				{Key: "prize", Value: prizeIdInt64},
			})
		}
	}
	router := gin.Default()
	router.GET("/events/:address", totalAmountCount())
	router.GET("/events", getAll())
	router.GET("/getprize/:address", getAllPrize())
	router.Run()
}
func totalAmountCount() gin.HandlerFunc {
	return func(c *gin.Context) {
		// get total amount by each user address
		var events []bson.M
		var totalAmount int64
		results, err := collection.Find(context.Background(), bson.M{})
		if err != nil {
			c.JSON(http.StatusInternalServerError, APIResponse{Message: "Error", Code: 1, Data: nil})
			return
		}
		for results.Next(context.Background()) {
			var event bson.M
			err := results.Decode(&event)
			if err != nil {
				c.JSON(http.StatusInternalServerError, APIResponse{Message: "Error", Code: 1, Data: nil})
				return
			}
			events = append(events, event)
		}
		for _, event := range events {
			if event["user"] == strings.ToLower(c.Param("address")) && event["type"] == "request" {
				totalAmount += event["amount"].(int64)
			}
		}
		c.JSON(http.StatusOK, APIResponse{Message: "Success", Code: 0, Data: totalAmount})
	}
}

func getAllPrize() gin.HandlerFunc {
	return func(c *gin.Context) {
		var events []bson.M
		address := strings.ToLower(c.Param("address"))
		// var totalSpin int64
		// var totalGafi float64
		cursor, err := collection.Aggregate(context.Background(), mongo.Pipeline{bson.D{
			{Key: "$match", Value: bson.D{
				{Key: "type", Value: "response"},
				{Key: "user", Value: address},
			}},
		}, bson.D{
			{Key: "$unwind", Value: "$prize"},
		}, bson.D{
			{Key: "$group", Value: bson.D{
				{Key: "_id", Value: bson.D{
					{Key: "prize", Value: "$prize"},
					{Key: "user", Value: "$user"},
				}},
				{Key: "count", Value: bson.D{
					{Key: "$sum", Value: 1},
				}},
			}},
		},
			bson.D{
				{Key: "$sort", Value: bson.D{
					{Key: "_id.prize", Value: 1},
				}},
			},
			bson.D{
				{Key: "$project", Value: bson.D{
					{Key: "user", Value: "$_id.user"},
					{Key: "prize", Value: bson.D{
						{Key: "value", Value: "$_id.prize"},
						{Key: "total", Value: "$count"},
					}},
				}},
			}, bson.D{
				{Key: "$group", Value: bson.D{
					{Key: "_id", Value: "$user"},
					{Key: "prize", Value: bson.D{
						{Key: "$push", Value: "$prize"},
					}},
				}},
			},
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, APIResponse{Message: "Error", Code: 1, Data: nil})
			return
		}
		if err = cursor.All(context.Background(), &events); err != nil {
			c.JSON(http.StatusInternalServerError, APIResponse{Message: "Error", Code: 1, Data: nil})
			return
		}
		// for i := 0; i < len(events[0]["prize"].(primitive.A)); i++ {
		// 	if events[0]["prize"].(primitive.A)[i].(primitive.M)["value"].(int64) == 1 {
		// 		totalGafi += float64(events[0]["prize"].(primitive.A)[i].(primitive.M)["total"].(int32)) * 0.1
		// 	} else if events[0]["prize"].(primitive.A)[i].(primitive.M)["value"].(int64) == 2 {
		// 		totalSpin += int64(events[0]["prize"].(primitive.A)[i].(primitive.M)["total"].(int32))
		// 	} else if events[0]["prize"].(primitive.A)[i].(primitive.M)["value"].(int64) == 3 {
		// 		totalGafi += float64(events[0]["prize"].(primitive.A)[i].(primitive.M)["total"].(int32)) * 0.25
		// 	} else if events[0]["prize"].(primitive.A)[i].(primitive.M)["value"].(int64) == 4 {
		// 		totalSpin += 2
		// 	} else if events[0]["prize"].(primitive.A)[i].(primitive.M)["value"].(int64) == 5 {
		// 		totalGafi += float64(events[0]["prize"].(primitive.A)[i].(primitive.M)["total"].(int32)) * 0.5
		// 	} else if events[0]["prize"].(primitive.A)[i].(primitive.M)["value"].(int64) == 6 {
		// 		totalGafi += float64(events[0]["prize"].(primitive.A)[i].(primitive.M)["total"].(int32)) * 0.15
		// 	} else if events[0]["prize"].(primitive.A)[i].(primitive.M)["value"].(int64) == 7 {
		// 		totalGafi += float64(events[0]["prize"].(primitive.A)[i].(primitive.M)["total"].(int32)) * 2.5
		// 	}
		// // }
		// message := fmt.Sprintf("Total spin: %d, Total GAFI: %f", totalSpin, totalGafi)
		// fmt.Println(message)
		c.JSON(http.StatusOK, APIResponse{Message: "Success", Code: 0, Data: events})
	}
}

func getAll() gin.HandlerFunc {
	return func(c *gin.Context) {
		var events []bson.M
		results, err := collection.Find(context.Background(), bson.M{})
		if err != nil {
			c.JSON(http.StatusInternalServerError, APIResponse{Message: "Error", Code: 1, Data: nil})
			return
		}
		for results.Next(context.Background()) {
			var event bson.M
			err := results.Decode(&event)
			if err != nil {
				c.JSON(http.StatusInternalServerError, APIResponse{Message: "Error", Code: 1, Data: nil})
				return
			}
			events = append(events, event)
		}
		c.JSON(http.StatusOK, APIResponse{Message: "Success", Code: 0, Data: events})
	}
}
