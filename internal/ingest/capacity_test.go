package ingest_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/RenJieWeia/iot-admin/internal/exception"
	"github.com/RenJieWeia/iot-admin/internal/ingest"
	"github.com/RenJieWeia/iot-admin/internal/meta"
	"github.com/RenJieWeia/iot-admin/internal/publish"
	"github.com/RenJieWeia/iot-admin/internal/store"
)

// 容量实测（批量契约 v2）：1 万条终端消息 x 15 个数据项。
// 运行：IOT_CAPACITY=1 go test ./internal/ingest -run TestCapacity -v
func TestCapacityBaseline10kMessages(t *testing.T) {
	if os.Getenv("IOT_CAPACITY") == "" {
		t.Skip("设置 IOT_CAPACITY=1 运行容量实测")
	}
	const (
		terminals   = 1000
		perTerminal = 10
		itemsPer    = 15
		workers     = 16
	)
	e := newEnv(t)
	ctx := context.Background()

	// 补齐到 15 个数据项（temp/power/hum/status/online 已由夹具建好）。
	for i := 6; i <= itemsPer; i++ {
		if _, err := e.pool.Exec(ctx, `
			insert into thing_model_property (type_id, item_code, name, value_type, unit, precision, required)
			values ($1, $2, $2, 'number', 'kW', 2, false)`, e.typeID, fmt.Sprintf("item%d", i)); err != nil {
			t.Fatalf("补物模型属性: %v", err)
		}
	}
	if _, err := e.pool.Exec(ctx, `
		insert into terminal (id, type_id, gateway_id, name, enabled)
		select 'CAP-' || g, $1, $2, 'cap-' || g, true from generate_series(1, $3) g`,
		e.typeID, testGatewayID, terminals); err != nil {
		t.Fatalf("批量建终端: %v", err)
	}

	proc := &ingest.Processor{
		DB:    e.pool,
		Meta:  meta.NewStore(meta.NewPGLoader(e.pool), meta.DefaultTTL, nil),
		Pub:   publish.Discard{},
		Exc:   exception.New(e.pool, e.clock.Now, quietLog),
		Locks: store.NewShardedLock(256),
		Now:   e.clock.Now,
		Log:   quietLog,
	}

	type job struct {
		terminal string
		seq      int
	}
	jobs := make(chan job, 256)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var accepted, rejected int
	start := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				at := baseTime.Add(time.Duration(j.seq) * time.Minute)
				items := make([]item, 0, itemsPer)
				items = append(items,
					it("temp", float64(20+j.seq), "C", at),
					it("power", float64(j.seq)+0.5, "kW", at),
					it("hum", float64(50+j.seq), "", at),
					it("status", "on", "", at),
					it("online", true, "", at),
				)
				for i := 6; i <= itemsPer; i++ {
					items = append(items, it(fmt.Sprintf("item%d", i), float64(i)+0.25, "kW", at))
				}
				payload := batch(testGatewayID, termOf(j.terminal, items...))
				res, err := proc.Handle(ctx, testGatewayID, payload)
				mu.Lock()
				if err != nil {
					rejected++
				} else {
					accepted++
					for _, r := range res.Results {
						if !r.Accepted {
							rejected++
						}
					}
				}
				mu.Unlock()
			}
		}()
	}
	for term := 1; term <= terminals; term++ {
		for seq := 0; seq < perTerminal; seq++ {
			jobs <- job{terminal: fmt.Sprintf("CAP-%d", term), seq: seq}
		}
	}
	close(jobs)
	wg.Wait()
	writeElapsed := time.Since(start)

	messages := terminals * perTerminal
	writes := messages * itemsPer
	if rejected != 0 {
		t.Fatalf("拒收消息数 = %d，期望 0（受理 %d）", rejected, accepted)
	}
	latestRows := e.count(`select count(*) from latest_value where terminal_id like 'CAP-%'`)

	var latestBytes int64
	if err := e.pool.QueryRow(ctx, `select pg_total_relation_size('latest_value')`).Scan(&latestBytes); err != nil {
		t.Fatalf("统计磁盘占用: %v", err)
	}

	perRow := float64(latestBytes) / float64(writes)
	hours := 24 * 30 * 24.0 // 720 天口径
	extrapolatedRows := 20000.0 * itemsPer * hours
	extrapolatedBytes := extrapolatedRows * perRow

	t.Logf("== 容量实测（%d 条消息 / %d 个数据项 / %d 台终端）==", messages, itemsPer, terminals)
	t.Logf("受理消息: %d，数据项写入: %d", accepted, writes)
	t.Logf("写入耗时: %s，吞吐: %.1f 消息/秒，%.0f 行/秒", writeElapsed,
		float64(messages)/writeElapsed.Seconds(), float64(writes)/writeElapsed.Seconds())
	t.Logf("latest_value 行数: %d（期望 %d）", latestRows, terminals*itemsPer)
	t.Logf("磁盘占用: latest=%d B，每行 %.1f B（含索引）", latestBytes, perRow)
	t.Logf("外推 20000 台 x 15 项 x 24 个月: %.2f 亿行，约 %.0f GB", extrapolatedRows/1e8, extrapolatedBytes/1e9)

	if latestRows != terminals*itemsPer {
		t.Fatalf("latest_value 行数 = %d，期望 %d", latestRows, terminals*itemsPer)
	}
}
