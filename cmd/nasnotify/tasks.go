package main

import (
	"log"
	"time"

	"nasnotify-go/internal/config"
	"nasnotify-go/internal/nas"
)

func runTasksLoop() {
	time.Sleep(2 * time.Second)
	for {
		startedAt := time.Now()

		log.Println("--- start notification polling task ---")
		nas.ProcessUGreen()

		waitForConfiguredInterval(startedAt, configuredNotificationIntervalMinutes, config.DefaultIntervalMinutes)
	}
}

func runSystemStatusTasksLoop() {
	time.Sleep(3 * time.Second)
	for {
		waitForConfiguredInterval(time.Now(), configuredSystemStatusIntervalMinutes, config.DefaultSystemStatusIntervalMinutes)
		log.Println("--- trigger scheduled system status push ---")
		nas.PushUGreenSystemStatus()
	}
}

func configuredNotificationIntervalMinutes() float64 {
	return config.GetConfigSnapshot().IntervalMinutes
}

func configuredSystemStatusIntervalMinutes() float64 {
	return config.GetConfigSnapshot().SystemStatusIntervalMinutes
}

func waitForConfiguredInterval(startedAt time.Time, intervalMinutes func() float64, fallbackMinutes float64) {
	for {
		duration := durationFromMinutes(intervalMinutes(), fallbackMinutes)
		remaining := duration - time.Since(startedAt)
		if remaining <= 0 {
			return
		}
		if remaining > 30*time.Second {
			remaining = 30 * time.Second
		}
		time.Sleep(remaining)
	}
}

func durationFromMinutes(minutes, fallbackMinutes float64) time.Duration {
	if minutes <= 0 {
		minutes = fallbackMinutes
	}
	return time.Duration(minutes * float64(time.Minute))
}
