package abletonosc

import "fmt"

func AsFloat64(v interface{}) (float64, error) {
	switch t := v.(type) {
	case float32:
		return float64(t), nil
	case float64:
		return t, nil
	case int32:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case int:
		return float64(t), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}

func AsInt(v interface{}) (int, error) {
	switch t := v.(type) {
	case int32:
		return int(t), nil
	case int64:
		return int(t), nil
	case int:
		return t, nil
	case float32:
		return int(t), nil
	case float64:
		return int(t), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to int", v)
	}
}

func AsBool(v interface{}) (bool, error) {
	switch t := v.(type) {
	case bool:
		return t, nil
	case int32:
		return t != 0, nil
	case int64:
		return t != 0, nil
	case int:
		return t != 0, nil
	case float32:
		return t != 0, nil
	case float64:
		return t != 0, nil
	default:
		return false, fmt.Errorf("cannot convert %T to bool", v)
	}
}
