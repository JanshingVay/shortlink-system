wrk -t4 -c100 -d10s -s benchmark_post.lua http://localhost:8080/api/v1/shorten

docker compose up -d