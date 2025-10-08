package logger

import "go.uber.org/zap"

var log *zap.Logger

func SetType(mode string) {
	if mode == "dev" {
		log, _ = zap.NewDevelopment()
	} else {
		log, _ = zap.NewProduction()
	}
}

func toFields(xs ...interface{}) []zap.Field {
	out := make([]zap.Field, 0, len(xs))
	i := 0
	for i < len(xs) {
		switch v := xs[i].(type) {
		case zap.Field:
			out = append(out, v)
			i++
		case map[string]interface{}:
			for k, val := range v {
				out = append(out, zap.Any(k, val))
			}
			i++
		case string:
			if i+1 < len(xs) {
				out = append(out, zap.Any(v, xs[i+1]))
				i += 2
			} else {
				out = append(out, zap.Any(v, nil))
				i++
			}
		default:
			// fallback: add with empty key
			out = append(out, zap.Any("", v))
			i++
		}
	}
	return out
}

var callback func(urgency int, msg string, fields ...interface{})

func SetCallBack(f func(urgency int, msg string, fields ...interface{})) {
	callback = f
}

func Info(msg string, fields ...interface{}) {
	if callback != nil {
		callback(0, msg, fields...)
	}
	log.Info(msg, toFields(fields...)...)
}

func Error(msg string, fields ...interface{}) {
	if callback != nil {
		callback(1, msg, fields...)
	}
	log.Error(msg, toFields(fields...)...)
}

func Warn(msg string, fields ...interface{}) {
	if callback != nil {
		callback(2, msg, fields...)
	}
	log.Warn(msg, toFields(fields...)...)
}

func Debug(msg string, fields ...interface{}) {
	if callback != nil {
		callback(3, msg, fields...)
	}
	log.Debug(msg, toFields(fields...)...)
}

func Sync() {
	log.Sync()
}
