package base62

import (
	"errors"
	"strings"
)

const (
	// 62 个字符的字典表。
	// 如果不想让别人轻易猜出连续的短链，可以把这个字符串打乱（洗牌），这样生成的短链看起来就是完全随机的。
	// 这里使用标准顺序。
	alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	base     = uint64(len(alphabet))
)

// Encode 将 64 位无符号整数转换为 Base62 字符串
func Encode(num uint64) string {
	if num == 0 {
		return string(alphabet[0])
	}

	var bytes []byte
	// 核心逻辑：不断取余和整除
	for num > 0 {
		rem := num % base
		bytes = append(bytes, alphabet[rem])
		num = num / base
	}

	// 因为最先计算出的是低位，所以需要将切片翻转才能得到正确的字符串
	reverse(bytes)
	return string(bytes)
}

// Decode 将 Base62 字符串反向解析为 64 位整数（用于根据短链查找对应的雪花 ID）
func Decode(str string) (uint64, error) {
	var num uint64
	length := len(str)

	for i := range length {
		char := str[i]
		// 查找字符在字典表中的索引位置
		index := strings.IndexByte(alphabet, char)
		if index == -1 {
			return 0, errors.New("包含非法字符，不是合法的短链")
		}
		// 进制转换：num = num * 62 + index
		num = num*base + uint64(index)
	}
	return num, nil
}

// reverse 原地翻转 byte 切片，避免额外的内存分配，极致压榨性能
func reverse(b []byte) {
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
}
