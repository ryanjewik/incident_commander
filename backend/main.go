package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"github.com/ryanjewik/incident_commander/backend/config"
	"github.com/ryanjewik/incident_commander/backend/handlers"
	"github.com/ryanjewik/incident_commander/backend/router"
	"github.com/ryanjewik/incident_commander/backend/services"

	//confluent test
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

func ReadConfig() kafka.ConfigMap {
	// reads the client configuration from client.properties
	// and returns it as a key-value map
	m := kafka.ConfigMap{}

	file, err := os.Open("client.properties")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open file: %s", err)
		os.Exit(1)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "#") && len(line) != 0 {
			kv := strings.Split(line, "=")
			parameter := strings.TrimSpace(kv[0])
			value := strings.TrimSpace(kv[1])
			m[parameter] = value
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Failed to read file: %s", err)
		os.Exit(1)
	}

	return m
}

func produce(topic string, config kafka.ConfigMap) {
	// creates a new producer instance
	p, _ := kafka.NewProducer(&config)

	// go-routine to handle message delivery reports and
	// possibly other event types (errors, stats, etc)
	go func() {
		for e := range p.Events() {
			switch ev := e.(type) {
			case *kafka.Message:
				if ev.TopicPartition.Error != nil {
					fmt.Printf("Failed to deliver message: %v\n", ev.TopicPartition)
				} else {
					fmt.Printf("Produced event to topic %s: key = %-10s value = %s\n",
						*ev.TopicPartition.Topic, string(ev.Key), string(ev.Value))
				}
			}
		}
	}()

	// produces a sample message to the user-created topic
	p.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Key:            []byte("key"),
		Value:          []byte("value"),
	}, nil)

	// send any outstanding or buffered messages to the Kafka broker and close the connection
	p.Flush(15 * 1000)
	p.Close()
}

func consume(topic string, config kafka.ConfigMap) {
	// sets the consumer group ID and offset
	config["group.id"] = "go-group-1"
	config["auto.offset.reset"] = "earliest"

	// creates a new consumer and subscribes to your topic
	consumer, _ := kafka.NewConsumer(&config)
	consumer.SubscribeTopics([]string{topic}, nil)

	run := true
	for run {
		// consumes messages from the subscribed topic and prints them to the console
		e := consumer.Poll(1000)
		switch ev := e.(type) {
		case *kafka.Message:
			// application-specific processing
			fmt.Printf("Consumed event from topic %s: key = %-10s value = %s\n",
				*ev.TopicPartition.Topic, string(ev.Key), string(ev.Value))
		case kafka.Error:
			fmt.Fprintf(os.Stderr, "%% Error: %v\n", ev)
			run = false
		}
	}

	// closes the consumer connection
	consumer.Close()
}

func main() {
	// Load .env file - this must come BEFORE config.Load()
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	topic := "topic_0"
	kafkaconfig := ReadConfig()

	go produce(topic, kafkaconfig)

	// Run consumer in a goroutine so it doesn't block the server
	go consume(topic, kafkaconfig)

	cfg := config.Load()

	if cfg.Port == "" {
		cfg.Port = "8080"
		log.Println("defaulting:8080")
	}

	firebaseService, err := services.NewFirebaseService(cfg.FirebaseCredentialsPath)
	if err != nil {
		panic(err)
	}
	defer firebaseService.Close()

	userService := services.NewUserService(firebaseService)

	app := handlers.NewApp(cfg)

	r := gin.Default()

	router.Register(r, app, userService)

	log.Printf("Starting server on port %s", cfg.Port)
	r.Run(":" + cfg.Port)
}
