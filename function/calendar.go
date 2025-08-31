package function

import (
	"fmt"
	"time"
)

// --- 核心数据区 ---

// lunarData 存储了从1900年到2050年的农历信息
// 每一项代表一年：{总天数, 闰月(0表示无)}
var lunarData = []int{
	// 1900-1909
	0x04bd8, 0x04ae0, 0x0a570, 0x054d5, 0x0d260, 0x0d950, 0x16554, 0x056a0, 0x09ad0, 0x055d2,
	// 1910-1919
	0x04ae0, 0x0a5b6, 0x0a4d0, 0x0d250, 0x1d255, 0x0b540, 0x0d6a0, 0x0ada2, 0x095b0, 0x14977,
	// 1920-1929
	0x04970, 0x0a4b0, 0x0b4b5, 0x06a50, 0x06d40, 0x1ab54, 0x02b60, 0x09570, 0x052f2, 0x04970,
	// 1930-1939
	0x06566, 0x0d4a0, 0x0ea50, 0x06e95, 0x05ad0, 0x02b60, 0x186e3, 0x092e0, 0x1c8d7, 0x0c950,
	// 1940-1949
	0x0d4a0, 0x1d8a6, 0x0b550, 0x056a0, 0x1a5b4, 0x025d0, 0x092d0, 0x0d2b2, 0x0a950, 0x0b557,
	// 1950-1959
	0x06ca0, 0x0b550, 0x15355, 0x04da0, 0x0a5b0, 0x14573, 0x052b0, 0x0a9a8, 0x0e950, 0x06aa0,
	// 1960-1969
	0x0aea6, 0x0ab50, 0x04b60, 0x0aae4, 0x0a570, 0x05260, 0x0f263, 0x0d950, 0x05b57, 0x056a0,
	// 1970-1979
	0x096d0, 0x04dd5, 0x04ad0, 0x0a4d0, 0x0d4d4, 0x0d250, 0x0d558, 0x0b540, 0x0b6a0, 0x195a6,
	// 1980-1989
	0x095b0, 0x049b0, 0x0a974, 0x0a4b0, 0x0b27a, 0x06a50, 0x06d40, 0x0af46, 0x0ab60, 0x09570,
	// 1990-1999
	0x04af5, 0x04970, 0x064b0, 0x074a3, 0x0ea50, 0x06b58, 0x055c0, 0x0ab60, 0x096d5, 0x092e0,
	// 2000-2009
	0x0c960, 0x0d954, 0x0d4a0, 0x0da50, 0x07552, 0x056a0, 0x0abb7, 0x025d0, 0x092d0, 0x0cab5,
	// 2010-2019
	0x0a950, 0x0b4a0, 0x0baa4, 0x0ad50, 0x055d9, 0x04ba0, 0x0a5b0, 0x15176, 0x052b0, 0x0a930,
	// 2020-2029
	0x07954, 0x06aa0, 0x0ad50, 0x05b52, 0x04b60, 0x0a6e6, 0x0a4e0, 0x0d260, 0x0ea65, 0x0d530,
	// 2030-2039
	0x05aa0, 0x076a3, 0x096d0, 0x04afb, 0x04ad0, 0x0a4d0, 0x1d0b6, 0x0d250, 0x0d520, 0x0dd45,
	// 2040-2049
	0x0b5a0, 0x056d0, 0x055b2, 0x049b0, 0x0a577, 0x0a4b0, 0x0aa50, 0x1b255, 0x06d20, 0x0ada0,
	// 2050
	0x14b63,
}

// LunarDate 用于存储计算结果
type LunarDate struct {
	Year       int
	Month      int
	Day        int
	IsLeap     bool
	GanZhiDay  string
	GanZhiTime string
}

// --- 核心算法区 ---

const epochYear = 1900
const epochMonth = 1
const epochDay = 31

var epochDate = time.Date(epochYear, epochMonth, epochDay, 0, 0, 0, 0, time.UTC)

const epochGanZhiDayIndex = 16 // 1900-01-31 是庚寅日

func SolarToLunar(t time.Time) (*LunarDate, error) {
	if t.Year() < 1900 || t.Year() > 2050 {
		return nil, fmt.Errorf("日期超出范围 (1900-2050)")
	}
	daysDiff := int(t.UTC().Sub(epochDate).Hours() / 24)
	var lunarYear, lunarMonth, lunarDay int
	var isLeapMonth bool
	offset := daysDiff
	yearIndex := 0
	for ; yearIndex < len(lunarData); yearIndex++ {
		daysInYear := getLunarYearDays(yearIndex)
		if offset < daysInYear {
			lunarYear = epochYear + yearIndex
			break
		}
		offset -= daysInYear
	}
	leapMonth := getLeapMonth(yearIndex)
	for month := 1; month <= 13; month++ {
		isCurrentLeap := false
		daysInMonth := 0
		if month == leapMonth+1 {
			isCurrentLeap = true
			daysInMonth = getLeapMonthDays(yearIndex)
		} else {
			actualMonth := month
			if leapMonth > 0 && month > leapMonth {
				actualMonth--
			}
			daysInMonth = getLunarMonthDays(yearIndex, actualMonth)
		}
		if offset < daysInMonth {
			lunarMonth = month
			lunarDay = offset + 1
			isLeapMonth = isCurrentLeap
			break
		}
		offset -= daysInMonth
	}
	if lunarMonth > 12 {
		lunarMonth -= 1
	}
	ganZhiDayIndex := (epochGanZhiDayIndex + daysDiff) % 60
	ganZhiDay := tianGan[ganZhiDayIndex%10] + diZhi[ganZhiDayIndex%12]
	timeZhiIndex := (t.Hour() + 1) / 2 % 12
	return &LunarDate{Year: lunarYear, Month: lunarMonth, Day: lunarDay, IsLeap: isLeapMonth, GanZhiDay: ganZhiDay, GanZhiTime: diZhi[timeZhiIndex]}, nil
}

func getLeapMonth(yearIndex int) int { return lunarData[yearIndex] & 0xf }

// --- 辅助函数 ---
func getLunarYearDays(yearIndex int) int {
	sum := 348
	for i := 0x8000; i > 0x8; i >>= 1 {
		if (lunarData[yearIndex] & i) != 0 {
			sum++
		}
	}
	if (lunarData[yearIndex] & 0x10000) != 0 {
		sum += 30
	} else {
		sum += 29
	}
	return sum
}

func getLeapMonthDays(yearIndex int) int {
	if (lunarData[yearIndex] & 0x10000) != 0 {
		return 30
	}
	return 29
}

func getLunarMonthDays(yearIndex, month int) int {
	if (lunarData[yearIndex] & (0x10000 >> uint(month))) != 0 {
		return 30
	}
	return 29
}
