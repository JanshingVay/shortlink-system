-- benchmark_mixed.lua
-- 混合读写压测脚本 - 模拟真实短链接业务场景
--
-- 业务模型:
--   读写比例: 9:1 (90% 读, 10% 写)
--   写请求:   80% 全新链接, 20% 重复链接
--   读请求:   80% 存在的短码, 20% 不存在的短码
--
-- 使用前先运行种子数据脚本:
--   bash scripts/seed_benchmark.sh http://localhost:8080 1000 seed_codes.txt
--
-- 压测命令:
--   wrk -t4 -c100 -d30s -s benchmark_mixed.lua http://localhost:8080

-- ==========================================
-- 可调参数：真实业务比例
-- ==========================================
local READ_RATIO = 0.9
local READ_EXIST_RATIO = 0.8
local WRITE_NEW_RATIO = 0.8

-- ==========================================
-- 种子数据
-- ==========================================
local existing_codes = {}
local code_count = 0

local dup_urls = {
    "https://github.com/weijianxing/shortlink-system",
    "https://www.google.com/search?q=shortlink+benchmark",
    "https://stackoverflow.com/questions/tagged/go",
    "https://redis.io/docs/latest/develop/",
    "https://gorm.io/docs/",
    "https://gin-gonic.com/docs/",
    "https://kubernetes.io/docs/",
    "https://www.docker.com/resources/",
    "https://prometheus.io/docs/",
    "https://grafana.com/docs/",
}
local dup_url_count = #dup_urls
local new_url_counter = 0
local CODE_LENGTH = 10
local charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

-- ==========================================
-- 跨线程汇总用
-- ==========================================
local threads = {}

function setup(thread)
    thread:set("cnt_2xx", 0)
    thread:set("cnt_3xx", 0)
    thread:set("cnt_404", 0)
    thread:set("cnt_429", 0)
    thread:set("cnt_other", 0)
    table.insert(threads, thread)
end

-- ==========================================
-- 每个线程初始化
-- ==========================================
function init(args)
    local seed_file = "seed_codes.txt"
    local f = io.open(seed_file, "r")
    if f then
        for line in f:lines() do
            local code = string.match(line, "^[A-Za-z0-9]+$")
            if code and #code == 10 then
                table.insert(existing_codes, code)
            end
        end
        f:close()
        code_count = #existing_codes
    end

    if code_count == 0 then
        print("[benchmark] 警告: seed_codes.txt 未找到或为空")
    else
        print("[benchmark] 已加载 " .. code_count .. " 条种子短码")
    end

    -- 线程局部计数器
    my_2xx = 0
    my_3xx = 0
    my_404 = 0
    my_429 = 0
    my_other = 0
end

-- ==========================================
-- 辅助函数
-- ==========================================
function random_code()
    local code = ""
    for i = 1, CODE_LENGTH do
        local rand_idx = math.random(1, #charset)
        code = code .. string.sub(charset, rand_idx, rand_idx)
    end
    return code
end

function unique_url()
    new_url_counter = new_url_counter + 1
    return "https://benchmark.example.com/u/" .. new_url_counter
end

-- ==========================================
-- 每次请求 - 用 wrk.format() 构造完整 HTTP 请求
-- 这是 wrk 官方推荐解决 GET/POST 混合的方式
-- ==========================================
request = function()
    local rand_val = math.random()

    if rand_val < READ_RATIO then
        local path
        if code_count > 0 and math.random() < READ_EXIST_RATIO then
            local idx = math.random(1, code_count)
            path = "/api/v1/decode/" .. existing_codes[idx]
        else
            path = "/api/v1/decode/" .. random_code()
        end
        return wrk.format("GET", path)
    else
        local body
        if math.random() < WRITE_NEW_RATIO then
            body = '{"long_url":"' .. unique_url() .. '"}'
        else
            local dup_idx = math.random(1, dup_url_count)
            body = '{"long_url":"' .. dup_urls[dup_idx] .. '"}'
        end
        return wrk.format("POST", "/api/v1/encode",
            {["Content-Type"] = "application/json"}, body)
    end
end

-- ==========================================
-- 响应处理：在各自线程内计数并同步给全局账本
-- ==========================================
response = function(status, headers, body)
    if status >= 200 and status < 300 then
        my_2xx = my_2xx + 1
        wrk.thread:set("cnt_2xx", my_2xx)
    elseif status >= 300 and status < 400 then
        my_3xx = my_3xx + 1
        wrk.thread:set("cnt_3xx", my_3xx)
    elseif status == 404 then
        my_404 = my_404 + 1
        wrk.thread:set("cnt_404", my_404)
    elseif status == 429 then
        my_429 = my_429 + 1
        wrk.thread:set("cnt_429", my_429)
    else
        my_other = my_other + 1
        wrk.thread:set("cnt_other", my_other)
    end
end

-- ==========================================
-- 压测结束报告
-- ==========================================
function done(summary, latency)
    local total = summary.requests
    if total == 0 then
        io.write("无请求数据\n")
        return
    end

    local count_2xx = 0
    local count_3xx = 0
    local count_404 = 0
    local count_429 = 0
    local count_other = 0

    for _, thread in ipairs(threads) do
        count_2xx = count_2xx + tonumber(thread:get("cnt_2xx"))
        count_3xx = count_3xx + tonumber(thread:get("cnt_3xx"))
        count_404 = count_404 + tonumber(thread:get("cnt_404"))
        count_429 = count_429 + tonumber(thread:get("cnt_429"))
        count_other = count_other + tonumber(thread:get("cnt_other"))
    end

    local read_est = math.floor(total * READ_RATIO)
    local write_est = total - read_est
    local read_exist_est = math.floor(read_est * READ_EXIST_RATIO)
    local read_not_exist_est = read_est - read_exist_est
    local write_new_est = math.floor(write_est * WRITE_NEW_RATIO)
    local write_dup_est = write_est - write_new_est

    io.write("------------------------------\n")
    io.write("       混合读写压测报告\n")
    io.write("------------------------------\n")
    io.write(string.format("压测时长:       %ds\n", summary.duration / 1000000))
    io.write(string.format("总请求数:       %d\n", total))
    io.write(string.format("QPS:            %.1f\n", total / (summary.duration / 1000000)))
    io.write(string.format("平均延迟:       %.2fms\n", latency.mean / 1000))
    io.write(string.format("P99 延迟:       %.2fms\n", latency:percentile(99) / 1000))
    io.write("------------------------------\n")
    io.write("-- 请求分布 (按脚本比例) --\n")
    io.write(string.format("读请求 (%.0f%%):    %d\n", READ_RATIO * 100, read_est))
    io.write(string.format("  已存在短码 (%.0f%%): %d\n", READ_EXIST_RATIO * 100, read_exist_est))
    io.write(string.format("  不存在短码 (%.0f%%): %d\n", (1 - READ_EXIST_RATIO) * 100, read_not_exist_est))
    io.write(string.format("写请求 (%.0f%%):    %d\n", (1 - READ_RATIO) * 100, write_est))
    io.write(string.format("  全新链接 (%.0f%%):   %d\n", WRITE_NEW_RATIO * 100, write_new_est))
    io.write(string.format("  重复链接 (%.0f%%):   %d\n", (1 - WRITE_NEW_RATIO) * 100, write_dup_est))
    io.write("------------------------------\n")
    io.write("-- HTTP 状态码分布 --\n")
    io.write(string.format("2xx 成功:       %d (%.1f%%)\n", count_2xx, count_2xx / total * 100))
    if count_3xx > 0 then
        io.write(string.format("3xx 重定向:     %d (%.1f%%)\n", count_3xx, count_3xx / total * 100))
    end
    io.write(string.format("404 不存在:     %d (%.1f%%) [预期 ~%d]\n", count_404, count_404 / total * 100, read_not_exist_est))
    io.write(string.format("429 限流:       %d (%.1f%%)\n", count_429, count_429 / total * 100))
    if count_other > 0 then
        io.write(string.format("其他错误:       %d (%.1f%%)\n", count_other, count_other / total * 100))
    end
    io.write("------------------------------\n")
end
