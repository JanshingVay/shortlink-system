package base62

import (
	"errors"
	"strings"
)

// 使用乱序字典防止 ID 被恶意预测和遍历抓取 (Security through obscurity)
const defaultAlphabet = "vPh7zQwE9oA4cK8sY2mD6gU1lX5rN3tB0iJfFkOqHjuyxWnCZbVMIaLdTReSGp"
const base = uint64(62)

type Base62 struct {
	alphabet string
}

// NewBase62 创建 Base62 编解码器实例
// 核心设计：默认使用乱序字典（防 ID 被恶意预测/遍历），也支持自定义合规字典
// 安全考量：默认字典非顺序排列，通过「隐匿安全」降低短链/ID 被枚举抓取的风险
//
// 参数：
//
//	customAlphabet: 可选自定义编码字典（传参需满足长度=62，否则使用默认字典）
//
// 返回值：
//
//	*Base62: Base62 编解码器实例（非 nil）
//
// 示例：
//
//	// 使用默认乱序字典（推荐，兼顾安全）
//	b62 := NewBase62()
//	// 使用自定义字典（需确保62个唯一字符，仅特殊场景使用）
//	b62 := NewBase62("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")
func NewBase62(customAlphabet ...string) *Base62 {
	alpha := defaultAlphabet
	if len(customAlphabet) > 0 && len(customAlphabet[0]) == 62 {
		alpha = customAlphabet[0]
	}
	return &Base62{alphabet: alpha}
}

// Encode 将 uint64 类型数值编码为 Base62 字符串
// 核心优化：零额外内存分配（仅最终字符串化时一次不可避免拷贝），避免堆逃逸
// 性能亮点：
//  1. 基于 uint64 最大值特性（62进制下最多11位），使用栈上局部数组 buf 替代堆内存分配
//  2. 从数组尾部反向填充，减少数据移动开销
//  3. 仅在返回时做一次 []byte -> string 转换，无额外内存逃逸
//
// 参数：
//
//	num: 待编码的无符号64位整数（非负）
//
// 返回值：
//
//	string: Base62 编码后的字符串，num=0 时返回字典第一个字符
//
// 安全背景：
//
//	依赖实例化时的乱序字典（默认），可防止 ID/短链等场景下的数值被恶意预测
func (b *Base62) Encode(num uint64) string {
	if num == 0 {
		return string(b.alphabet[0])
	}

	// uint64 最大值在 62 进制下最多 11 位，使用局部数组避免逃逸到堆上
	var buf [11]byte
	idx := 11

	for num > 0 {
		idx--
		buf[idx] = b.alphabet[num%base]
		num /= base
	}

	// 只在最后生成 string 时产生一次不可避免的内存拷贝
	return string(buf[idx:])
}

// Decode 将 Base62 编码字符串解码为 uint64 数值
// 核心逻辑：逐字符查找其在编码字典中的索引，按 62 进制规则还原数值
// 安全约束：仅支持当前实例字典内的字符，非法字符直接返回错误
//
// 参数：
//
//	str: Base62 编码字符串（仅允许包含当前实例字典中的字符，空字符串解码结果为 0）
//
// 返回值：
//
//	uint64: 解码后的无符号64位整数，str为空时返回0
//	error: 解码失败错误（字符串含字典外字符时返回 "invalid base62 character"）
//
// 注意事项：
//  1. 解码结果受实例字典影响，与编码时的字典不一致会导致结果错误
//  2. 若解码字符串对应数值超过 uint64 最大值，会发生无提示的数值溢出
func (b *Base62) Decode(str string) (uint64, error) {
	var num uint64
	for i := 0; i < len(str); i++ {
		index := strings.IndexByte(b.alphabet, str[i])
		if index == -1 {
			return 0, errors.New("invalid base62 character")
		}
		num = num*base + uint64(index)
	}
	return num, nil
}
