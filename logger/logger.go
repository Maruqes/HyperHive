package logger

import (
	"fmt"

	"go.uber.org/zap"
)

var (
	log      *zap.Logger
	callback func(urgency int, msg string, fields ...interface{})
)

// init garante que, mesmo antes de SetType, não rebenta (faz logs "no-op").
func init() {
	log = zap.NewNop()
}

// SetType inicializa o logger em modo dev ou prod
// e aplica AddCallerSkip(1) para o caller correto.
func SetType(mode string) {
	var (
		base *zap.Logger
		err  error
	)

	if mode == "dev" {
		base, err = zap.NewDevelopment()
	} else {
		base, err = zap.NewProduction()
	}
	if err != nil {
		panic(err)
	}

	// Skip 1 frame para as linhas de log apontarem para
	// a função que chamou logger.Info/Error/etc.
	log = base.WithOptions(zap.AddCallerSkip(1))
}

// SetCallBack permite interceptar todos os logs (por exemplo,
// enviar para WebSocket/UI) com nível numérico:
// 0=Info, 1=Error, 2=Warn, 3=Debug.
func SetCallBack(f func(urgency int, msg string, fields ...interface{})) {
	callback = f
}

// toFields converte argumentos variádicos em []zap.Field.
// Suporta:
//   - zap.Field diretamente
//   - map[string]interface{}{...}
//   - pares "key", value
//   - fallback: arg_0, arg_1, ...
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
			// tratar como "key", value
			if i+1 < len(xs) {
				out = append(out, zap.Any(v, xs[i+1]))
				i += 2
			} else {
				// key sem value → nil
				out = append(out, zap.Any(v, nil))
				i++
			}

		default:
			// fallback: gerar um nome de key
			out = append(out, zap.Any(fmt.Sprintf("arg_%d", i), v))
			i++
		}
	}
	return out
}

// --------- Funções estruturadas (recomendado em zap) ---------

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

// --------- Versões estilo printf (%s, %d, etc.) ---------

func Infof(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if callback != nil {
		callback(0, msg, args...)
	}
	// Usa Sugar para format string
	log.Sugar().Infof(format, args...)
}

func Errorf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if callback != nil {
		callback(1, msg, args...)
	}
	log.Sugar().Errorf(format, args...)
}

func Warnf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if callback != nil {
		callback(2, msg, args...)
	}
	log.Sugar().Warnf(format, args...)
}

func Debugf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if callback != nil {
		callback(3, msg, args...)
	}
	log.Sugar().Debugf(format, args...)
}

// Sync deve ser chamado no shutdown do programa para flush de buffers.
func Sync() error {
	if log == nil {
		return nil
	}
	return log.Sync()
}