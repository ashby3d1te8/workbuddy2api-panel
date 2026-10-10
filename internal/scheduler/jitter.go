// jitter.go 调度侧错峰与抖动（反封禁移植：本地基线 → panel）。
//
// 为什么要它：travel/activity 的账号间节流是**恒定** 800ms，而签到、keepalive 与
// 余额后台刷新此前**完全没有节流**；runBatch 又把同槽的多个任务族**并行同秒**派发。
// 从同一出口 IP 看，这是「同秒起步 + 恒定节拍」的机器特征——上游频控按密度判罚，
// 固定相位会周期性聚团。
//
// 与 internal/server/backoff.go 的 jitterDur 同口径：d<=0 原样返回，于是测试把
// 各延时变量置 0 即自动关闭抖动，不拖慢用例（与既有 fastTravel/fastActivity 同约定）。
package scheduler

import (
	"math/rand/v2"
	"time"
)

// schedSpread 每个任务族派发前的随机起始偏移上限：把同槽并行的任务族彼此错开
// （09:00 签到与旅行同槽）。测试在 scheddelay_test.go 的 init 里置 0。
var schedSpread = 3 * time.Minute

// jitterPacing 给账号间节流施加 [0.5d, 1.5d) 的均匀抖动（均值仍为 d）。
// d<=0 原样返回：测试置 0 时零抖动。
func jitterPacing(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	return d/2 + time.Duration(rand.Int64N(int64(d)))
}

// schedOffset 返回某任务族派发前的随机起始偏移。pacing<=0（该族无节流，或测试
// 已把节流置 0）或 schedSpread<=0 时返回 0，保证测试零等待。
func schedOffset(pacing time.Duration) time.Duration {
	if pacing <= 0 || schedSpread <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(schedSpread)))
}

// pacingForKind 返回任务族自己的账号间节流值，供 schedOffset 判定是否施加错峰：
// 借它做闸门，测试把节流置 0 时错峰也自动为 0。
func pacingForKind(k taskKind) time.Duration {
	switch k {
	case taskCheckin:
		return checkinAccountDelay
	case taskTravel:
		return travelAccountDelay
	case taskActivity:
		return activityAccountDelay
	case taskKeepalive:
		return keepaliveAccountDelay
	}
	return 0
}
