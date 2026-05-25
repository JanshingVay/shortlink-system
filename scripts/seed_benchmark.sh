#!/bin/bash
# 压测种子数据脚本
# 预先生成一批短码写入 seed_codes.txt，供混合压测脚本读取
#
# 注意: 请先关闭限流启动服务
#   go run cmd/server/main.go --rate-limit=false

BASE_URL="${1:-http://localhost:8080}"
SEED_COUNT="${2:-1000}"
OUTPUT_FILE="${3:-seed_codes.txt}"

echo "正在生成 ${SEED_COUNT} 条种子数据到 ${OUTPUT_FILE} ..."
> "$OUTPUT_FILE"

SUCCESS=0
FAIL=0
SHOW_DEBUG=3  # 前几次失败时打印详细信息

for i in $(seq 1 $SEED_COUNT); do
    LONG_URL="https://example.com/seed/${i}-$(date +%s%N)"

    RESP=$(curl -s -X POST "${BASE_URL}/api/v1/encode" \
        -H 'Content-Type: application/json' \
        -d "{\"long_url\":\"${LONG_URL}\"}")

    # 从 JSON 中提取 short_url 字段，再从 URL 末尾提取短码
    # 返回格式示例: {"code":200,"short_url":"http://localhost:8080/7aUx9GVxgA"}
    SHORT_CODE=$(echo "$RESP" \
        | sed -n 's/.*"short_url":"[^"]*\/\([A-Za-z0-9]\{10\}\)".*/\1/p')

    if [ -n "$SHORT_CODE" ] && [ ${#SHORT_CODE} -eq 10 ]; then
        echo "$SHORT_CODE" >> "$OUTPUT_FILE"
        SUCCESS=$((SUCCESS + 1))
    else
        FAIL=$((FAIL + 1))
        if [ $FAIL -le $SHOW_DEBUG ]; then
            echo "  [调试] 第 ${i} 条失败, 服务返回: ${RESP:0:200}"
            if echo "$RESP" | grep -q '"error"'; then
                echo "  [提示] 请确保启动时关闭了限流: go run cmd/server/main.go --rate-limit=false"
            fi
        fi
    fi

    # 进度
    if [ $((i % 100)) -eq 0 ]; then
        echo "  进度: ${i}/${SEED_COUNT} (成功: ${SUCCESS}, 失败: ${FAIL})"
    fi
done

echo "完成! 成功: ${SUCCESS}, 失败: ${FAIL}"
echo "种子数据已写入: ${OUTPUT_FILE}"
