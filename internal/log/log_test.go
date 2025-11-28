package log

import "testing"

func TestJSONLoggerLevels(t *testing.T){
    l := New("debug")
    l.Debug("d", map[string]any{"x":1})
    l.Info("i", nil)
    l.Warn("w", nil)
    l.Error("e", nil)
}

