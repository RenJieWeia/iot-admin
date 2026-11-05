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
	"github.com/RenJieWeia/iot-admin/internal/publish"
	"github.com/RenJieWeia/iot-admin/internal/store"
	"github.com/RenJieWeia/iot-admin/internal/validate"
)

// 容量实测：1 万条终端消息 x 15 个数据项，记录写入速率、快照行数与磁盘占用。
// 需要真实 PostgreSQL，运行方式：IOT_CAPACITY=1 go test ./internal/ingest -run TestCapacity -v
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
	hour := hourOf(baseTime)

	// 补齐到 15 个数据项（temp/power 已由夹具建好）。
	for i := 3; i <= itemsPer; i++ {
		if _, err := e.pool.Exec(ctx, `
			insert into thing_model_property (type_id, item_code, name, value_type, unit, precision, required)
			values ($1, $2, $3, 'number', 'kW', 2, true)`, e.typeID, fmt.Sprintf("item%d", i), fmt.Sprintf("数据项%d", i)); err != nil {
			t.Fatalf("补物模型属性: %v", err)
		}
	}
	// 20000 台规模的终端池先建 1000 台做实测。
	if _, err := e.pool.Exec(ctx, `
		insert into terminal (id, type_id, gateway_id, name, enabled)
		select 'CAP-' || g, $1, 'GW-CAP', 'cap-' || g, true from generate_series(1, $2) g`,
		e.typeID, terminals); err != nil {
		t.Fatalf("批量建终端: %v", err)
	}

	proc := &ingest.Processor{
		DB:    e.pool,
		Reg:   validate.NewRegistry(e.pool),
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
				its := make([]item, 0, itemsPer)
				at := baseTime.Add(time.Duration(j.seq) * time.Minute)
				for i := 1; i <= itemsPer; i++ {
					code := fmt.Sprintf("item%d", i)
					if i == 1 {
						code = "temp"
					} else if i == 2 {
						code = "power"
					}
					its = append(its, item{Code: code, Value: float64(j.seq) + float64(i)/100, Unit: "C", Time: at})
				}
				payload := build(j.terminal, "GW-CAP", its...)
				res := proc.Handle(ctx, "GW-CAP", payload)
				mu.Lock()
				if res.Accepted {
					accepted++
				} else {
					rejected++
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
	if accepted != messages {
		t.Fatalf("受理消息 = %d，期望 %d（拒收 %d）", accepted, messages, rejected)
	}
	latestRows := e.count(`select count(*) from latest_value where terminal_id like 'CAP-%'`)
	stagingRows := e.count(`select count(*) from hourly_staging where terminal_id like 'CAP-%'`)

	e.clock.Set(closeOf(hour).Add(time.Second))
	settleStart := time.Now()
	settled, err := e.settler.Run(ctx)
	settleElapsed := time.Since(settleStart)
	if err != nil {
		t.Fatalf("结算失败: %v", err)
	}

	// 磁盘占用：分区表按分区求和，索引计入 pg_total_relation_size。
	var snapBytes, latestBytes, stagingBytes int64
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("统计磁盘占用: %v", err)
		}
	}
	must(e.pool.QueryRow(ctx, `
		select coalesce(sum(pg_total_relation_size(c.oid)), 0) from pg_inherits i
		join pg_class c on c.oid = i.inhrelid
		where i.inhparent = 'hourly_snapshot'::regclass`).Scan(&snapBytes))
	must(e.pool.QueryRow(ctx, `select pg_total_relation_size('latest_value')`).Scan(&latestBytes))
	must(e.pool.QueryRow(ctx, `select pg_total_relation_size('hourly_staging')`).Scan(&stagingBytes))

	perRow := float64(snapBytes) / float64(settled)
	// 外推：20000 台 x 15 项 / 小时 x 24 个月（17520 小时）
	months := 24.0
	hours := months * 30 * 24
	extrapolatedRows := 20000.0 * itemsPer * hours
	extrapolatedBytes := extrapolatedRows * perRow

	t.Logf("== 容量实测（%d 条消息 / %d 个数据项 / %d 台终端）==", messages, itemsPer, terminals)
	t.Logf("受理消息: %d，数据项写入: %d", accepted, writes)
	t.Logf("写入耗时: %s，吞吐: %.1f 消息/秒，%.0f 行/秒", writeElapsed, float64(messages)/writeElapsed.Seconds(), float64(writes)/writeElapsed.Seconds())
	t.Logf("latest_value 行数: %d，hourly_staging 行数: %d", latestRows, stagingRows)
	t.Logf("结算: %d 行，耗时 %s", settled, settleElapsed)
	t.Logf("磁盘占用: snapshot=%d B latest=%d B staging=%d B", snapBytes, latestBytes, stagingBytes)
	t.Logf("快照每行占用: %.1f B（含索引）", perRow)
	t.Logf("外推 20000 台 x 15 项 x 24 个月: %.2f 亿行，约 %.0f GB", extrapolatedRows/1e8, extrapolatedBytes/1e9)

	if settled != int64(terminals*itemsPer) {
		t.Fatalf("快照行数 = %d，期望 %d", settled, terminals*itemsPer)
	}
	if stagingRows != terminals*itemsPer {
		t.Fatalf("暂存行数 = %d，期望 %d", stagingRows, terminals*itemsPer)
	}
}
