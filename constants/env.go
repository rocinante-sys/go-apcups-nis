package constants

var ApcDebug = GetEnv("APC_DEBUG", "false")

var ApcUpsName = GetEnv("APC_UPSNAME", "UPS")
var ApcModel = GetEnv("APC_MODEL", "Back-UPS")
var ApcNomPow = GetEnv("APC_NOMPOWER", "1000")
var ApcDriver = GetEnv("APC_DRIVER", "go-apcups-nis")
var ApcFirmware = GetEnv("APC_FIRMWARE", "1.0")
var ApcSerialNo = GetEnv("APC_SERIALNO", "EMU123456")
var ApcVersion = GetEnv("APC_VERSION", "1.0.0 (go-apcups-nis)")
var ApcCable = GetEnv("APC_CABLE", "Ethernet Link")
var ApcUpsMode = GetEnv("APC_CABLE", "Stand Alone")
