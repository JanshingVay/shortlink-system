package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/rabbitmq/amqp091-go"
)

const (
	// ExchangeName 缓存更新交换机
	ExchangeName = "shortlink.cache.direct"
	// QueueName 缓存更新队列
	QueueName = "shortlink.cache.update"
	// RoutingKey 路由键
	RoutingKey = "shortlink.cache.update"
)

// CacheUpdateMessage 缓存更新消息体
type CacheUpdateMessage struct {
	ShortCode string `json:"short_code"`
	LongURL   string `json:"long_url"`
	URLHash   string `json:"url_hash"`
}

// RabbitMQ RabbitMQ 客户端封装
type RabbitMQ struct {
	conn    *amqp091.Connection
	channel *amqp091.Channel
	url     string
}

// NewRabbitMQ 创建 RabbitMQ 客户端并声明交换机、队列和绑定。
func NewRabbitMQ(url string) (*RabbitMQ, error) {
	mq := &RabbitMQ{url: url}
	if err := mq.connect(); err != nil {
		return nil, err
	}
	return mq, nil
}

func (r *RabbitMQ) connect() error {
	conn, err := amqp091.Dial(r.url)
	if err != nil {
		return fmt.Errorf("连接 RabbitMQ 失败: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("创建 RabbitMQ Channel 失败: %w", err)
	}

	// 声明交换机
	if err := ch.ExchangeDeclare(
		ExchangeName,       // name
		"direct",           // type
		true,               // durable
		false,              // auto-deleted
		false,              // internal
		false,              // no-wait
		nil,                // arguments
	); err != nil {
		return fmt.Errorf("声明交换机失败: %w", err)
	}

	// 声明队列
	if _, err := ch.QueueDeclare(
		QueueName, // name
		true,      // durable
		false,     // delete when unused
		false,     // exclusive
		false,     // no-wait
		nil,       // arguments
	); err != nil {
		return fmt.Errorf("声明队列失败: %w", err)
	}

	// 绑定队列到交换机
	if err := ch.QueueBind(
		QueueName,      // queue name
		RoutingKey,     // routing key
		ExchangeName,   // exchange
		false,
		nil,
	); err != nil {
		return fmt.Errorf("绑定队列失败: %w", err)
	}

	r.conn = conn
	r.channel = ch
	return nil
}

// Publish 发布缓存更新消息到 RabbitMQ。
func (r *RabbitMQ) Publish(ctx context.Context, msg CacheUpdateMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %w", err)
	}

	return r.channel.PublishWithContext(
		ctx,
		ExchangeName, // exchange
		RoutingKey,   // routing key
		true,         // mandatory
		false,        // immediate
		amqp091.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp091.Persistent, // 消息持久化
		},
	)
}

// Consume 启动消费者，callback 处理单条消息。
func (r *RabbitMQ) Consume(ctx context.Context, callback func(CacheUpdateMessage) error) error {
	msgs, err := r.channel.Consume(
		QueueName, // queue
		"",        // consumer
		false,     // auto-ack，手动确认
		false,     // exclusive
		false,     // no-local
		false,     // no-wait
		nil,       // args
	)
	if err != nil {
		return fmt.Errorf("启动消费者失败: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d, ok := <-msgs:
			if !ok {
				log.Println("RabbitMQ 消费通道已关闭")
				return nil
			}

			var msg CacheUpdateMessage
			if err := json.Unmarshal(d.Body, &msg); err != nil {
				log.Printf("消息反序列化失败: %v", err)
				// 消息格式错误，直接拒绝并丢弃，避免无限重试
				d.Nack(false, false)
				continue
			}

			if err := callback(msg); err != nil {
				log.Printf("消费消息处理失败: %v", err)
				// 处理失败，重新入队重试
				d.Nack(false, true)
				continue
			}

			if err := d.Ack(false); err != nil {
				log.Printf("确认消息失败: %v", err)
			}
		}
	}
}

// Close 关闭 RabbitMQ 连接。
func (r *RabbitMQ) Close() error {
	if r.channel != nil {
		if err := r.channel.Close(); err != nil {
			return err
		}
	}
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}

// WaitUntilReady 轮询等待 RabbitMQ 可用，用于启动时容错。
func (r *RabbitMQ) WaitUntilReady(ctx context.Context, maxWait time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if err := r.channel.Qos(1, 0, false); err == nil {
			return true
		}
		time.Sleep(1 * time.Second)
	}
	return false
}
