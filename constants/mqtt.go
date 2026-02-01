package constants

var MqttServer = GetEnv("MQTT_SERVER", "tcp://127.0.0.1:1883")
var MqttDevice = GetEnv("MQTT_DEVICE", "go-apcups-nis")
var MqttUser = GetEnv("MQTT_USER", "user")
var MqttPass = GetEnv("MQTT_PASS", "pass")
