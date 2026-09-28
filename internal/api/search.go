// Package api — 索引查询 Handler
package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// handleSearchRecords 支持按 patient_id 或 time_range 查询区块。
// Query 参数：
//   - patient_id: 按患者 ID 精确查询
//   - start: 起始时间（ISO8601 或 Unix 纳秒）
//   - end: 结束时间（ISO8601 或 Unix 纳秒）
func (srv *AppServer) handleSearchRecords(c *gin.Context) {
	patientID := c.Query("patient_id")
	startStr := c.Query("start")
	endStr := c.Query("end")

	// 按 patient_id 查询
	if patientID != "" {
		blocks, err := srv.store.QueryByPatientID(patientID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"query_type": "patient_id",
			"patient_id": patientID,
			"count":      len(blocks),
			"blocks":     blocks,
		})
		return
	}

	// 按时间范围查询
	if startStr != "" && endStr != "" {
		start, end, err := parseTimeRange(startStr, endStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid time range: " + err.Error()})
			return
		}
		blocks, err := srv.store.QueryByTimeRange(start, end)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"query_type": "time_range",
			"start":      start,
			"end":        end,
			"count":      len(blocks),
			"blocks":     blocks,
		})
		return
	}

	c.JSON(http.StatusBadRequest, gin.H{
		"error": "missing query parameters: provide patient_id or start+end",
	})
}

// parseTimeRange 解析时间范围参数。
// 支持 ISO8601 格式（如 2024-05-13T00:00:00Z）或 Unix 纳秒时间戳。
func parseTimeRange(startStr, endStr string) (int64, int64, error) {
	parse := func(s string) (int64, error) {
		// 尝试解析为数字（Unix 纳秒）
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, nil
		}
		// 尝试解析为 ISO8601
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			// 尝试解析为日期格式
			t, err = time.Parse("2006-01-02", s)
			if err != nil {
				return 0, err
			}
		}
		return t.UnixNano(), nil
	}

	start, err := parse(startStr)
	if err != nil {
		return 0, 0, err
	}
	end, err := parse(endStr)
	if err != nil {
		return 0, 0, err
	}
	return start, end, nil
}
