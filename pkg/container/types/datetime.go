// Copyright 2021 Matrix Origin
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package types

import (
	"fmt"
	gotime "time"
	"unsafe"

	"github.com/matrixorigin/matrixone/pkg/errno"
	"github.com/matrixorigin/matrixone/pkg/sql/errors"
)

const (
	secsPerMinute = 60
	secsPerHour   = 60 * secsPerMinute
	secsPerDay    = 24 * secsPerHour
	//secsPerWeek   = 7 * secsPerDay
	microSecondBitMask = 0xfffff
	MaxDatetimeYear    = 9999
	MinDatetimeYear    = 1
)

// The higher 44 bits holds number of seconds since January 1, year 1 in Gregorian
// calendar, and lower 20 bits holds number of microseconds

const (
	//tsMask         = ^uint64(0) >> 1
	hasMonotonic = 1 << 63
	//unixToInternal = (1969*365 + 1969/4 - 1969/100 + 1969/400) * secsPerDay
	wallToInternal = (1884*365 + 1884/4 - 1884/100 + 1884/400) * secsPerDay

	minHourInDay, maxHourInDay           = 0, 23
	minMinuteInHour, maxMinuteInHour     = 0, 59
	minSecondInMinute, maxSecondInMinute = 0, 59
)

var (
	errIncorrectDatetimeValue = errors.New(errno.DataException, "Incorrect datetime value")
)

func (dt Datetime) String() string {
	y, m, d, _ := dt.ToDate().Calendar(true)
	hour, minute, sec := dt.Clock()
	return fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d", y, m, d, hour, minute, sec)
}

func (dt Datetime) String2(precision int32) string {
	y, m, d, _ := dt.ToDate().Calendar(true)
	hour, minute, sec := dt.Clock()
	if precision > 0 {
		msec := int64(dt) & 0xfffff
		msecInstr := fmt.Sprintf("%06d\n", msec)
		msecInstr = msecInstr[:precision]

		return fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d"+"."+msecInstr, y, m, d, hour, minute, sec)
	}
	return fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d", y, m, d, hour, minute, sec)
}

// ParseDatetime will parse a string to be a Datetime
// Support Format:
// 1. all the Date value
// 2. yyyy-mm-dd hh:mm:ss(.msec)
// 3. yyyymmddhhmmss(.msec)
// during parsing, the Datetime value will be rounded(away from zero) to the predefined precision, for example:
// Datetime(3) input string   					parsing result
// 				"1999-09-09 11:11:11.1234"		"1999-09-09 11:11:11.123"
//				"1999-09-09 11:11:11.1235"		"1999-09-09 11:11:11.124"
// 				"1999-09-09 11:11:11.9994"      "1999-09-09 11:11:11.999"
// 				"1999-09-09 11:11:11.9995"      "1999-09-09 11:11:12.000"
func ParseDatetime(s string, precision int32) (Datetime, error) {
	if len(s) < 14 {
		if d, err := ParseDate(s); err == nil {
			return d.ToTime(), nil
		}
		return -1, errIncorrectDatetimeValue
	}
	var year int32
	var month, day, hour, minute, second uint8
	var msec uint32 = 0
	var carry uint32 = 0
	var err error

	year = int32(s[0]-'0')*1000 + int32(s[1]-'0')*100 + int32(s[2]-'0')*10 + int32(s[3]-'0')
	if s[4] == '-' {
		if len(s) < 19 {
			return -1, errIncorrectDatetimeValue
		}
		month = (s[5]-'0')*10 + (s[6] - '0')
		if s[7] != '-' {
			return -1, errIncorrectDatetimeValue
		}
		day = (s[8]-'0')*10 + (s[9] - '0')
		if s[10] != ' ' {
			return -1, errIncorrectDatetimeValue
		}
		if !validDate(year, month, day) {
			return -1, errIncorrectDatetimeValue
		}
		hour = (s[11]-'0')*10 + (s[12] - '0')
		if s[13] != ':' {
			return -1, errIncorrectDatetimeValue
		}
		minute = (s[14]-'0')*10 + (s[15] - '0')
		if s[16] != ':' {
			return -1, errIncorrectDatetimeValue
		}
		second = (s[17]-'0')*10 + (s[18] - '0')
		if !validTimeInDay(hour, minute, second) {
			return -1, errIncorrectDatetimeValue
		}
		if len(s) > 19 {
			if len(s) > 20 && s[19] == '.' {
				msecStr := s[20:]
				msec, carry, err = getMsec(msecStr, precision)
				if err != nil {
					return -1, errIncorrectDatetimeValue
				}
			} else {
				return -1, errIncorrectDatetimeValue
			}
		}
	} else {
		month = (s[4]-'0')*10 + (s[5] - '0')
		day = (s[6]-'0')*10 + (s[7] - '0')
		hour = (s[8]-'0')*10 + (s[9] - '0')
		minute = (s[10]-'0')*10 + (s[11] - '0')
		second = (s[12]-'0')*10 + (s[13] - '0')
		if len(s) > 14 {
			if len(s) > 15 && s[14] == '.' {
				msecStr := s[15:]
				msec, carry, err = getMsec(msecStr, precision)
				if err != nil {
					return -1, errIncorrectDatetimeValue
				}
			} else {
				return -1, errIncorrectDatetimeValue
			}
		}
	}
	result := FromClock(year, month, day, hour, minute, second+uint8(carry), msec)
	return result, nil
}

// validTimeInDay return true if hour, minute and second can be a time during a day
func validTimeInDay(h, m, s uint8) bool {
	if h < minHourInDay || h > maxHourInDay {
		return false
	}
	if m < minMinuteInHour || m > maxMinuteInHour {
		return false
	}
	if s < minSecondInMinute || s > maxSecondInMinute {
		return false
	}
	return true
}

// UTC turn local datetime to utc datetime
func (dt Datetime) UTC() Datetime {
	return Datetime((dt.sec() - localTZ) << 20)
}

func (dt Datetime) UnixTimestamp() int64 {
	return dt.sec() - unixEpoch
}

func FromUnix(time int64) Datetime {
	return Datetime((time + unixEpoch) << 20)
}

func Now() Datetime {
	t := gotime.Now()
	wall := *(*uint64)(unsafe.Pointer(&t))
	ext := *(*int64)(unsafe.Pointer(uintptr(unsafe.Pointer(&t)) + unsafe.Sizeof(wall)))
	var sec, nsec int64
	if wall&hasMonotonic != 0 {
		sec = int64(wall<<1>>31) + wallToInternal
		nsec = int64(wall << 34 >> 34)
	} else {
		sec = ext
		nsec = int64(wall)
	}
	return Datetime((sec << 20) + nsec/1000)
}

func (dt Datetime) ToDate() Date {
	return Date((dt.sec()) / secsPerDay)
}

func (dt Datetime) Clock() (hour, min, sec int8) {
	t := (dt.sec()) % secsPerDay
	hour = int8(t / secsPerHour)
	min = int8(t % secsPerHour / secsPerMinute)
	sec = int8(t % secsPerMinute)
	return
}

func (dt Datetime) Sec() int8 {
	_, _, sec := dt.Clock()
	return sec
}

func (dt Datetime) Minute() int8 {
	_, minute, _ := dt.Clock()
	return minute
}

func (dt Datetime) Hour() int8 {
	hour, _, _ := dt.Clock()
	return hour
}

func FromClock(year int32, month, day, hour, min, sec uint8, msec uint32) Datetime {
	days := FromCalendar(year, month, day)
	secs := int64(days)*secsPerDay + int64(hour)*secsPerHour + int64(min)*secsPerMinute + int64(sec)
	return Datetime((secs << 20) + int64(msec))
}

func (dt Datetime) ConvertToGoTime() gotime.Time {
	y, m, d, _ := dt.ToDate().Calendar(true)
	msec := dt.MicroSec()
	hour, min, sec := dt.Clock()
	return gotime.Date(int(y), gotime.Month(m), int(d), int(hour), int(min), int(sec), int(msec*1000), startupTime.Location())
}

func DatetimeToTimestamp(xs []Datetime, rs []Timestamp) ([]Timestamp, error) {
	localTZAligned := localTZ << 20
	xsInInt64 := *(*[]int64)(unsafe.Pointer(&xs))
	rsInInt64 := *(*[]int64)(unsafe.Pointer(&rs))
	for i, x := range xsInInt64 {
		rsInInt64[i] = x - localTZAligned
	}
	return rs, nil
}

func (dt Datetime) AddDateTime(date gotime.Time, addMsec, addSec, addMin, addHour, addDay, addMonth, addYear int64, isDate bool) (Datetime, bool) {
	date = date.Add(gotime.Duration(addMsec) * gotime.Microsecond)
	date = date.Add(gotime.Duration(addSec) * gotime.Second)
	date = date.Add(gotime.Duration(addMin) * gotime.Minute)
	date = date.Add(gotime.Duration(addHour) * gotime.Hour)
	// corner case: mysql: date_add('2022-01-31',interval 1 month) -> 2022-02-28
	// only in the month year year-month
	if addMonth != 0 || addYear != 0 {
		originDay := date.Day()
		newDate := date.AddDate(int(addYear), int(addMonth), int(addDay))
		newDay := newDate.Day()
		if originDay != newDay {
			maxDay := LastDay(uint16(newDate.Year()), uint8(newDate.Month()-1))
			addDay = int64(maxDay) - int64(originDay)
		}
	}
	date = date.AddDate(int(addYear), int(addMonth), int(addDay))

	if isDate {
		if !validDate(int32(date.Year()), uint8(date.Month()), uint8(date.Day())) {
			return 0, false
		}
	} else {
		if !validDatetime(int32(date.Year()), uint8(date.Month()), uint8(date.Day())) {
			return 0, false
		}
	}
	return FromClock(int32(date.Year()), uint8(date.Month()), uint8(date.Day()), uint8(date.Hour()), uint8(date.Minute()), uint8(date.Second()), uint32(date.Nanosecond()/1000)), true
}

// AddInterval
// now date or datetime use the function to add/sub date, we need a bool arg to tell isDate/isDatetime
// date/datetime have different regions, so we don't use same valid function
// return type bool means the if the date/datetime is valid
func (dt Datetime) AddInterval(nums int64, its IntervalType, isDate bool) (Datetime, bool) {
	goTime := dt.ConvertToGoTime()
	var addMsec, addSec, addMin, addHour, addDay, addMonth, addYear int64
	switch its {
	case MicroSecond:
		addMsec += nums
	case Second:
		addSec += nums
	case Minute:
		addMin += nums
	case Hour:
		addHour += nums
	case Day:
		addDay += nums
	case Week:
		addDay += 7 * nums
	case Month:
		addMonth += nums
	case Quarter:
		addMonth += 3 * nums
	case Year:
		addYear += nums
	}
	return dt.AddDateTime(goTime, addMsec, addSec, addMin, addHour, addDay, addMonth, addYear, isDate)
}

func (dt Datetime) MicroSec() int64 {
	return int64(dt) << 44 >> 44
}

func (dt Datetime) sec() int64 {
	return int64(dt) >> 20
}

func (dt Datetime) Year() uint16 {
	return dt.ToDate().Year()
}

func (dt Datetime) Month() uint8 {
	return dt.ToDate().Month()
}

func (dt Datetime) Day() uint8 {
	return dt.ToDate().Day()
}

func (dt Datetime) WeekOfYear() (int32, uint8) {
	return dt.ToDate().WeekOfYear()
}

func (dt Datetime) SecondMicrosecondStr() string {
	result := fmt.Sprintf("%02d", dt.Sec()) + "." + fmt.Sprintf("%06d", dt.MicroSec())
	return result
}

func (dt Datetime) MinuteMicrosecondStr() string {
	result := fmt.Sprintf("%02d", dt.Minute()) + ":" + fmt.Sprintf("%02d", dt.Sec()) + "." + fmt.Sprintf("%06d", dt.MicroSec())
	return result
}

func (dt Datetime) MinuteSecondStr() string {
	result := fmt.Sprintf("%02d", dt.Minute()) + ":" + fmt.Sprintf("%02d", dt.Sec())
	return result
}

func (dt Datetime) HourMicrosecondStr() string {
	result := fmt.Sprintf("%2d", dt.Hour()) + ":" + fmt.Sprintf("%02d", dt.Minute()) + ":" + fmt.Sprintf("%02d", dt.Sec()) + "." + fmt.Sprintf("%06d", dt.MicroSec())
	return result
}

func (dt Datetime) HourSecondStr() string {
	result := fmt.Sprintf("%2d", dt.Hour()) + ":" + fmt.Sprintf("%02d", dt.Minute()) + ":" + fmt.Sprintf("%02d", dt.Sec())
	return result
}

func (dt Datetime) HourMinuteStr() string {
	result := fmt.Sprintf("%2d", dt.Hour()) + ":" + fmt.Sprintf("%02d", dt.Minute())
	return result
}

func (dt Datetime) DayMicrosecondStr() string {
	result := fmt.Sprintf("%02d", dt.Day()) + " " + dt.HourMicrosecondStr()
	return result
}

func (dt Datetime) DaySecondStr() string {
	result := fmt.Sprintf("%02d", dt.Day()) + " " + dt.HourSecondStr()
	return result
}

func (dt Datetime) DayMinuteStr() string {
	result := fmt.Sprintf("%02d", dt.Day()) + " " + dt.HourMinuteStr()
	return result
}

func (dt Datetime) DayHourStr() string {
	result := fmt.Sprintf("%02d", dt.Day()) + " " + fmt.Sprintf("%02d", dt.Hour())
	return result
}

func (dt Datetime) YearMonthStr() string {
	result := fmt.Sprintf("%04d", dt.Year()) + " " + fmt.Sprintf("%02d", dt.Month())
	return result
}

// date[0001-01-01 00:00:00 to 9999-12-31 23:59:59]
func validDatetime(year int32, month, day uint8) bool {
	if year >= MinDatetimeYear && year <= MaxDatetimeYear {
		if MinMonthInYear <= month && month <= MaxMonthInYear {
			if day > 0 {
				if isLeap(year) {
					return day <= leapYearMonthDays[month-1]
				} else {
					return day <= flatYearMonthDays[month-1]
				}
			}
		}
	}
	return false
}
