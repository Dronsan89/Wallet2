// Команда loadgen — воспроизводимая проверка нагрузки для стека Wallet2.
//
// Режимы:
//   - kafka: генерирует валидные события и отправляет их в wallet.transactions;
//   - http:  нагружает HTTP-ручку кошелька и считает p50/p95/p99.
//
// Запуск (из каталога tools/loadgen):
//
//	go run . -mode kafka -events 50000 -brokers localhost:19092
//	go run . -mode http -url http://127.0.0.1:8080/api/v1/balance -token "<JWT>" -n 2000 -c 50
//
// Обычно вызывается через scripts/loadtest.ps1.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

func main() {
	mode := flag.String("mode", "http", "режим: kafka или http")

	// Kafka.
	brokers := flag.String("brokers", "localhost:19092", "брокеры через запятую")
	topic := flag.String("topic", "wallet.transactions", "топик")
	events := flag.Int("events", 50000, "сколько событий отправить")
	batch := flag.Int("batch", 500, "размер пачки при отправке")
	rate := flag.Int("rate", 0, "ограничение скорости, msg/s (0 — без ограничения)")

	// HTTP.
	url := flag.String("url", "http://127.0.0.1:8080/api/v1/balance", "URL для нагрузки")
	token := flag.String("token", "", "JWT для защищённой ручки")
	n := flag.Int("n", 2000, "всего HTTP-запросов")
	c := flag.Int("c", 50, "параллельных воркеров")

	flag.Parse()

	var err error
	switch strings.ToLower(*mode) {
	case "kafka":
		err = runKafka(*brokers, *topic, *events, *batch, *rate)
	case "http":
		err = runHTTP(*url, *token, *n, *c)
	default:
		err = fmt.Errorf("неизвестный режим %q (ожидается kafka или http)", *mode)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "loadgen: %v\n", err)
		os.Exit(1)
	}
}

// runKafka отправляет события пачками и печатает фактическую скорость.
func runKafka(brokers, topic string, total, batch, rate int) error {
	if total <= 0 {
		return fmt.Errorf("events должно быть > 0")
	}
	if batch <= 0 || batch > total {
		batch = total
	}

	writer := &kafka.Writer{
		Addr:         kafka.TCP(strings.Split(brokers, ",")...),
		Topic:        topic,
		Balancer:     &kafka.Hash{},
		RequiredAcks: kafka.RequireOne,
		BatchSize:    batch,
		BatchTimeout: 10 * time.Millisecond,
	}
	defer writer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// created_at фиксируем на старте: так latency в аналитике отражает
	// задержку доставки, а не наше время генерации.
	createdAt := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	// Уникальный префикс запуска: иначе одинаковые event_id из разных запусков
	// схлопнутся идемпотентностью приёмников и испортят замер.
	prefix := fmt.Sprintf("load-%d", time.Now().UnixNano())

	start := time.Now()

	for sent := 0; sent < total; sent += batch {
		size := batch
		if remaining := total - sent; remaining < size {
			size = remaining
		}

		msgs := make([]kafka.Message, 0, size)
		for i := 0; i < size; i++ {
			id := sent + i
			msgs = append(msgs, kafka.Message{
				Key:   []byte(fmt.Sprintf("%s-%d", prefix, id)),
				Value: []byte(eventJSON(prefix, id, createdAt)),
			})
		}

		if err := writer.WriteMessages(ctx, msgs...); err != nil {
			return fmt.Errorf("отправка пачки: %w", err)
		}

		// Держим заданную скорость: спим столько, сколько осталось до планового
		// времени отправки очередной пачки.
		if rate > 0 {
			planned := time.Duration(float64(sent+size) / float64(rate) * float64(time.Second))
			if sleep := planned - time.Since(start); sleep > 0 {
				time.Sleep(sleep)
			}
		}
	}

	elapsed := time.Since(start)
	actual := float64(total) / elapsed.Seconds()

	fmt.Printf("kafka: produced=%d elapsed=%.3fs rps=%.0f target_rate=%d\n",
		total, elapsed.Seconds(), actual, rate)

	return nil
}

// eventJSON формирует событие в формате кошелька.
func eventJSON(prefix string, id int, createdAt string) string {
	return fmt.Sprintf(`{"event_id":"%s-%d","transaction_id":"%s-tx-%d","user_id":"load-user","operation":"DEPOSIT","status":"SUCCESS","from_currency":"USD","to_currency":"","amount":500,"rate":90,"result_amount":500,"rub_equivalent":45000,"created_at":%q,"source":"gw-currency-wallet","schema_version":1}`,
		prefix, id, prefix, id, createdAt)
}

// runHTTP нагружает ручку и считает перцентили задержки.
func runHTTP(url, token string, total, concurrency int) error {
	if total <= 0 || concurrency <= 0 {
		return fmt.Errorf("n и c должны быть > 0")
	}

	transport := &http.Transport{
		MaxIdleConns:        concurrency,
		MaxIdleConnsPerHost: concurrency,
		MaxConnsPerHost:     concurrency,
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport}

	jobs := make(chan struct{})
	latencies := make([]time.Duration, 0, total)

	var mu sync.Mutex
	var errorsCount int

	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range jobs {
				req, err := http.NewRequest(http.MethodGet, url, nil)
				if err != nil {
					mu.Lock()
					errorsCount++
					mu.Unlock()

					continue
				}
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}

				t0 := time.Now()
				resp, err := client.Do(req)
				d := time.Since(t0)

				mu.Lock()
				if err != nil || resp == nil || resp.StatusCode >= 400 {
					errorsCount++
				} else {
					latencies = append(latencies, d)
				}
				mu.Unlock()

				if resp != nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
			}
		}()
	}

	for i := 0; i < total; i++ {
		jobs <- struct{}{}
	}
	close(jobs)
	wg.Wait()

	elapsed := time.Since(start)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	percentile := func(q float64) time.Duration {
		if len(latencies) == 0 {
			return 0
		}

		return latencies[int(float64(len(latencies)-1)*q)]
	}

	fmt.Printf("http: requests=%d rps=%.0f errors=%d p50=%s p95=%s p99=%s max=%s\n",
		total, float64(total)/elapsed.Seconds(), errorsCount,
		percentile(0.50).Round(time.Millisecond), percentile(0.95).Round(time.Millisecond),
		percentile(0.99).Round(time.Millisecond), percentile(1).Round(time.Millisecond))

	if errorsCount > 0 {
		return fmt.Errorf("ошибок запросов: %d", errorsCount)
	}

	return nil
}
