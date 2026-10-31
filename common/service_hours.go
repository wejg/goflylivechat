package common

import "time"

const ServiceHoursMessage = "客服已下班，服务时间为每天北京时间 10:00–18:00。您可以留言，我们将在工作时间回复。"

var serviceLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

func IsServiceTime(now time.Time) bool {
	hour := now.In(serviceLocation).Hour()
	return hour >= 10 && hour < 18
}
