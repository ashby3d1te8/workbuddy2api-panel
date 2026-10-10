package scheduler

// 测试口径：反封禁移植新增的节流与同槽错峰一律置 0（与既有 fastTravel/fastActivity
// 的单点做法等价，集中一处以免今后新增节流项漏改、用例白等）。
func init() {
	checkinAccountDelay = 0
	keepaliveAccountDelay = 0
	balanceAccountDelay = 0
	schedSpread = 0
}
