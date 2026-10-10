WEB_DIR = ./web
API_DIR = .
DEV_WEB_PORT ?= 5173
DEV_COMPOSE_FILE = docker-compose.dev.yml
DEV_POSTGRES_SERVICE = postgres
DEV_API_SERVICE = new-api
DEV_POSTGRES_DB = new-api
DEV_POSTGRES_USER = root
DEV_SQLITE_PATH ?= one-api.db

.PHONY: all build-web build-all-web start-api dev dev-api dev-api-rebuild dev-web reset-setup test

all: build-all-web start-api

build-web:
	@echo "Building web frontend..."
	@cd $(WEB_DIR) && bun install --frozen-lockfile
	@cd $(WEB_DIR) && DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$$(cat ../VERSION) bun run build

build-all-web: build-web

start-api:
	@echo "Starting api dev server..."
	@cd $(API_DIR) && go run main.go &

dev-api:
	@echo "Starting api services (docker)..."
	@docker compose -f $(DEV_COMPOSE_FILE) up -d

dev-api-rebuild:
	@echo "Rebuilding and starting api service (docker)..."
	@docker compose -f $(DEV_COMPOSE_FILE) up -d --build $(DEV_API_SERVICE)

dev-web:
	@echo "Starting web frontend dev server..."
	@echo "Web frontend: http://localhost:$(DEV_WEB_PORT)"
	@cd $(WEB_DIR) && bun install
	@cd $(WEB_DIR) && bun run dev -- --host 0.0.0.0 --port $(DEV_WEB_PORT)

dev: dev-api dev-web

# The main package embeds the ignored web/dist output and is covered after build-web.
test:
	@echo "Testing root Go module..."
	@root_module=$$(GOWORK=off go list -m); \
		root_packages=$$(GOWORK=off go list -e ./... | grep -vxF "$$root_module"); \
		GOWORK=off go test $$root_packages
	@echo "Testing relaykit Go module..."
	@cd relaykit && GOWORK=off go test ./...
	@echo "Testing tokenkit Go module..."
	@cd tokenkit && GOWORK=off go test ./...

reset-setup:
	@echo "Resetting local setup wizard state..."
	@if docker compose -f $(DEV_COMPOSE_FILE) ps --services --status running | grep -qx "$(DEV_POSTGRES_SERVICE)"; then \
		echo "Detected running docker dev PostgreSQL. Removing setup record and root users..."; \
		docker compose -f $(DEV_COMPOSE_FILE) exec -T $(DEV_POSTGRES_SERVICE) \
			psql -U $(DEV_POSTGRES_USER) -d $(DEV_POSTGRES_DB) \
			-c 'DELETE FROM setups;' \
			-c 'DELETE FROM users WHERE role = 100;' \
			-c "DELETE FROM options WHERE key IN ('SelfUseModeEnabled', 'DemoSiteEnabled');"; \
		echo "Restarting docker dev api so setup status is recalculated..."; \
		docker compose -f $(DEV_COMPOSE_FILE) restart $(DEV_API_SERVICE); \
	elif db_path="$${SQLITE_PATH:-$(DEV_SQLITE_PATH)}"; db_path="$${db_path%%\?*}"; [ -f "$$db_path" ]; then \
		db_path="$${SQLITE_PATH:-$(DEV_SQLITE_PATH)}"; \
		db_path="$${db_path%%\?*}"; \
		echo "Detected local SQLite database: $$db_path"; \
		sqlite3 "$$db_path" \
			"DELETE FROM setups; DELETE FROM users WHERE role = 100; DELETE FROM options WHERE key IN ('SelfUseModeEnabled', 'DemoSiteEnabled');"; \
		echo "SQLite setup state reset. Restart the local api process before testing the setup wizard."; \
	else \
		echo "No running docker dev PostgreSQL or local SQLite database found."; \
		echo "Start the dev stack with 'make dev-api', or set SQLITE_PATH/DEV_SQLITE_PATH to your local SQLite database."; \
		exit 1; \
	fi

# ==================== Docker 构建与部署 ====================
# 用法: make <命令> [ENV=local|prod]  (默认 ENV=local)
#
#   make server-init ENV=prod   初始化全新服务器 (安装 make/Docker/Swap)
#   make deploy ENV=local       本地构建镜像并用 Docker 运行 (http://localhost:3000)
#   make deploy ENV=prod        本地交叉构建镜像 -> 上传 -> 服务器 make deploy
#
# 架构: 本地交叉编译 (go+bun, 前端 dist 嵌入二进制, 无需本地 Docker) -> 上传
#       构建包 -> 服务器端 Makefile (deploy/Makefile.server) 构建镜像并用
#       compose 编排 new-api + PostgreSQL + Redis
#
# 前提条件:
#   1. 本地已安装 go 和 bun (brew install go bun); 本地 Docker 部署才需要 Docker
#   2. 已配置 SSH 免密登录: ssh-copy-id root@47.101.60.138
#
# 服务器配置在 env/<ENV>/.env 中 (SERVER_HOST/SERVER_USER/SERVER_PORT/APP_BASE...)

ENV ?= local

# 从 env/<ENV>/.env 读取服务器配置 (不存在则使用默认值)
-include env/$(ENV)/.env

IMAGE           ?= new-api
# 镜像版本: 默认取 git 提交号 (如 9310231b3, 有未提交改动带 -dirty 后缀),
# 每次迭代生成唯一 tag, 服务器保留历史镜像, 可用 VERSION=<tag> 回滚
VERSION         ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo latest)
IMAGE_TAR       ?= /tmp/$(IMAGE)-$(VERSION).tar.gz
IMAGE_TAR_BASE  := $(notdir $(IMAGE_TAR))
# 本地交叉编译产物暂存目录 (本地编译 -> 上传 -> 服务器构建镜像, 无需本地 Docker)
BIN_PKG_DIR     ?= /tmp/$(IMAGE)-pkg-$(VERSION)
GOOS            := $(word 1,$(subst /, ,$(PROD_PLATFORM)))
GOARCH          := $(word 2,$(subst /, ,$(PROD_PLATFORM)))

SERVER_PORT     ?= 22
APP_BASE        ?= /app/new-api
PROD_PLATFORM   ?= linux/amd64
PROD_PORT       ?= 3000
PROD_CONTAINER  ?= new-api
SWAP_SIZE       ?= 2G

# 官方发布镜像 (deploy-upstream 使用, 无需本地编译)
UPSTREAM_IMAGE    ?= calciumion/new-api
UPSTREAM_VERSION  ?= latest

LOCAL_PORT      ?= 3000
LOCAL_CONTAINER ?= new-api-local
LOCAL_DATA_DIR  ?= $(shell pwd)/data
LOCAL_LOGS_DIR  ?= $(shell pwd)/logs

RED := \e[0;31m
GREEN := \e[0;32m
YELLOW := \e[0;33m
NC := \e[0m

SSH := ssh -p $(SERVER_PORT)
SCP := scp -P $(SERVER_PORT)

.PHONY: help deploy deploy-upstream docker-build docker-run server-setup server-init prod-build prod-upload prod-run \
	docker-logs docker-stop docker-restart docker-status docker-clean test-connection

## 显示部署命令帮助
help:
	@printf "$(GREEN)new-api 构建部署$(NC)  用法: make <命令> ENV=local|prod\n"
	@printf "\n"
	@printf "$(YELLOW)【首次使用】$(NC)\n"
	@printf "  make deploy ENV=local      本地构建并运行 -> http://localhost:3000\n"
	@printf "  make server-init ENV=prod  初始化生产服务器 (只需执行一次)\n"
	@printf "  make deploy-upstream ENV=prod  发布官方镜像 (无需编译, 快速上线)\n"
	@printf "  make deploy ENV=prod       编译源码并部署到生产服务器 (做了代码修改后用这个)\n"
	@printf "\n"
	@printf "$(YELLOW)【日常运维】$(NC)ENV=local 操作本地, ENV=prod 操作生产\n"
	@printf "  make docker-logs ENV=...     查看日志\n"
	@printf "  make docker-status ENV=...   查看状态\n"
	@printf "  make docker-restart ENV=...  重启\n"
	@printf "  make docker-stop ENV=...     停止\n"
	@printf "  make docker-clean ENV=...    清理\n"
	@printf "\n"
	@printf "$(YELLOW)【版本与回滚】$(NC)镜像版本默认取 git 提交号, 可用 VERSION=<tag> 覆盖\n"
ifeq ($(ENV),local)
	@printf "  ssh <生产服务器> \"cd $(APP_BASE) && make versions\"                查看历史版本\n"
	@printf "  ssh <生产服务器> \"cd $(APP_BASE) && make deploy VERSION=<tag>\"     回滚到指定版本\n"
else
	@printf "  ssh $(SERVER_USER)@$(SERVER_HOST) \"cd $(APP_BASE) && make versions\"                查看历史版本\n"
	@printf "  ssh $(SERVER_USER)@$(SERVER_HOST) \"cd $(APP_BASE) && make deploy VERSION=<tag>\"     回滚到指定版本\n"
endif
	@printf "\n"
	@printf "$(YELLOW)【其他】$(NC)\n"
	@printf "  make test-connection ENV=prod  测试 SSH 连接\n"
	@printf "  分步命令 (一般不需要单独用): docker-build docker-run server-setup prod-build prod-upload prod-run\n"
	@printf "\n"
ifeq ($(ENV),local)
	@printf "当前: ENV=$(ENV) 容器=$(LOCAL_CONTAINER) 端口=$(LOCAL_PORT) 版本=$(VERSION)\n"
else
	@printf "当前: ENV=$(ENV) 服务器=$(SERVER_USER)@$(SERVER_HOST):$(SERVER_PORT) 目录=$(APP_BASE) 端口=$(PROD_PORT) 版本=$(VERSION)\n"
endif

## 构建并部署 [ENV=local 本地 Docker | ENV=prod 生产服务器]
deploy:
ifeq ($(ENV),local)
	@$(MAKE) docker-build docker-run
else
	@$(MAKE) prod-build server-setup prod-upload prod-run
endif

## 本地构建 Docker 镜像
docker-build:
	@printf "$(YELLOW)>>> 构建镜像 $(IMAGE):$(VERSION)...$(NC)\n"
	docker build -t $(IMAGE):$(VERSION) .
	@printf "$(GREEN)>>> 镜像构建完成$(NC)\n"

## 本地启动容器 (默认 SQLite, 数据在 ./data, 日志在 ./logs)
docker-run:
	@printf "$(YELLOW)>>> 启动本地容器 $(LOCAL_CONTAINER)...$(NC)\n"
	@mkdir -p $(LOCAL_DATA_DIR) $(LOCAL_LOGS_DIR)
	-@docker stop $(LOCAL_CONTAINER) 2>/dev/null
	-@docker rm $(LOCAL_CONTAINER) 2>/dev/null
	docker run -d --name $(LOCAL_CONTAINER) --restart unless-stopped \
		-p $(LOCAL_PORT):3000 \
		-v $(LOCAL_DATA_DIR):/data \
		-v $(LOCAL_LOGS_DIR):/app/logs \
		$(IMAGE):$(VERSION) --log-dir /app/logs
	@printf "$(GREEN)>>> 本地服务已启动: http://localhost:$(LOCAL_PORT)$(NC)\n"

## (prod) 同步服务器 make 环境 (安装 make + 上传服务器端 Makefile/compose/nginx.conf/deploy.env/ssl)
server-setup:
	@printf "$(YELLOW)>>> 同步服务器 make 环境 -> $(SERVER_HOST):$(APP_BASE)...$(NC)\n"
	$(SSH) $(SERVER_USER)@$(SERVER_HOST) "command -v make >/dev/null 2>&1 || dnf install -y make; mkdir -p $(APP_BASE)"
	$(SSH) $(SERVER_USER)@$(SERVER_HOST) "if [ -f $(APP_BASE)/.env ] && grep -q '^SERVER_HOST=' $(APP_BASE)/.env && ! grep -q '^POSTGRES_PASSWORD=' $(APP_BASE)/.env; then rm -f $(APP_BASE)/.env && echo '>>> 已清理旧版 .env (make 配置迁移到 deploy.env)'; fi"
	$(SCP) deploy/Makefile.server $(SERVER_USER)@$(SERVER_HOST):$(APP_BASE)/Makefile
	$(SCP) deploy/docker-compose.server.yml $(SERVER_USER)@$(SERVER_HOST):$(APP_BASE)/docker-compose.yml
	$(SCP) deploy/nginx.conf $(SERVER_USER)@$(SERVER_HOST):$(APP_BASE)/nginx.conf
	$(SCP) env/$(ENV)/.env $(SERVER_USER)@$(SERVER_HOST):$(APP_BASE)/deploy.env
	@if [ -d env/$(ENV)/ssl ]; then \
		printf "$(YELLOW)>>> 上传 SSL 证书 env/$(ENV)/ssl/...$(NC)\n"; \
		$(SSH) $(SERVER_USER)@$(SERVER_HOST) "mkdir -p $(APP_BASE)/ssl && chmod 700 $(APP_BASE)/ssl"; \
		$(SCP) env/$(ENV)/ssl/* $(SERVER_USER)@$(SERVER_HOST):$(APP_BASE)/ssl/; \
	else \
		printf "$(RED)>>> 警告: env/$(ENV)/ssl/ 不存在, nginx 将因缺证书无法启动$(NC)\n"; \
		printf "$(YELLOW)>>> 请放入 xiaoyule.com.cn.pem / xiaoyule.com.cn.key 后重试$(NC)\n"; \
	fi
	@printf "$(GREEN)>>> 服务器 make 环境就绪$(NC)\n"

## (prod) 初始化全新服务器 (make 环境 + Docker + 日志轮转 + Swap + 目录)
server-init: server-setup
	@printf "$(YELLOW)>>> 执行服务器初始化 (SWAP_SIZE=$(SWAP_SIZE))...$(NC)\n"
	$(SSH) $(SERVER_USER)@$(SERVER_HOST) "cd $(APP_BASE) && make init SWAP_SIZE=$(SWAP_SIZE)"
	@printf "$(GREEN)>>> 服务器初始化完成, 可执行 make deploy ENV=prod$(NC)\n"

## (prod) 发布官方镜像 [UPSTREAM_VERSION=vX.Y.Z 默认 latest, 无需本地编译]
deploy-upstream: server-setup
	@printf "$(YELLOW)>>> 发布官方镜像 $(UPSTREAM_IMAGE):$(UPSTREAM_VERSION) 到 $(SERVER_HOST)...$(NC)\n"
	$(SSH) $(SERVER_USER)@$(SERVER_HOST) "cd $(APP_BASE) && make deploy-upstream UPSTREAM_IMAGE=$(UPSTREAM_IMAGE) UPSTREAM_VERSION=$(UPSTREAM_VERSION)"
	@printf "$(GREEN)>>> 完成: http://$(SERVER_HOST):$(PROD_PORT)$(NC)\n"

## (prod) 本地交叉编译二进制并打包 (前端 dist 已嵌入二进制, 无需本地 Docker)
prod-build:
	@printf "$(YELLOW)>>> 构建前端...$(NC)\n"
	@export PATH="/opt/homebrew/bin:/usr/local/bin:$$PATH" && $(MAKE) build-web
	@printf "$(YELLOW)>>> 交叉编译 $(GOOS)/$(GOARCH) 二进制...$(NC)\n"
	@rm -rf $(BIN_PKG_DIR) && mkdir -p $(BIN_PKG_DIR)
	export PATH="/opt/homebrew/bin:/usr/local/bin:$$PATH" && \
		CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) GOWORK=off GOEXPERIMENT=greenteagc \
		go build -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=$(VERSION)'" -o $(BIN_PKG_DIR)/new-api .
	@cp deploy/Dockerfile.runtime $(BIN_PKG_DIR)/Dockerfile
	@cp LICENSE NOTICE THIRD-PARTY-LICENSES.md $(BIN_PKG_DIR)/
	@printf "$(YELLOW)>>> 打包到 $(IMAGE_TAR)...$(NC)\n"
	@tar -czf $(IMAGE_TAR) -C $(BIN_PKG_DIR) new-api Dockerfile LICENSE NOTICE THIRD-PARTY-LICENSES.md
	@rm -rf $(BIN_PKG_DIR)
	@printf "$(GREEN)>>> 完成: $(IMAGE_TAR) ($$(du -h $(IMAGE_TAR) | cut -f1))$(NC)\n"

## (prod) 上传构建包到服务器
prod-upload:
	@printf "$(YELLOW)>>> 上传到 $(SERVER_USER)@$(SERVER_HOST):$(APP_BASE)...$(NC)\n"
	$(SSH) $(SERVER_USER)@$(SERVER_HOST) "mkdir -p $(APP_BASE)/data $(APP_BASE)/logs"
	$(SCP) $(IMAGE_TAR) $(SERVER_USER)@$(SERVER_HOST):$(APP_BASE)/
	@printf "$(GREEN)>>> 上传完成$(NC)\n"

## (prod) 服务器加载镜像并重启容器 (远程执行 make deploy)
prod-run:
	@printf "$(YELLOW)>>> 在服务器上部署...$(NC)\n"
	$(SSH) $(SERVER_USER)@$(SERVER_HOST) "cd $(APP_BASE) && make deploy VERSION=$(VERSION)"
	@printf "$(GREEN)>>> 部署完成: http://$(SERVER_HOST):$(PROD_PORT)$(NC)\n"
	@printf "$(YELLOW)>>> 提示: 服务器环境变量可写入 $(APP_BASE)/.env.runtime (如 SQL_DSN/REDIS_CONN_STRING), 再执行 make docker-restart ENV=prod$(NC)\n"

## 查看容器日志 [ENV=local|prod]
docker-logs:
ifeq ($(ENV),local)
	docker logs --tail=100 -f $(LOCAL_CONTAINER)
else
	$(SSH) $(SERVER_USER)@$(SERVER_HOST) "cd $(APP_BASE) && make logs"
endif

## 停止容器 [ENV=local|prod]
docker-stop:
ifeq ($(ENV),local)
	-@docker stop $(LOCAL_CONTAINER)
else
	$(SSH) $(SERVER_USER)@$(SERVER_HOST) "cd $(APP_BASE) && make stop"
endif
	@printf "$(GREEN)>>> 已停止$(NC)\n"

## 重启容器 [ENV=local|prod]
docker-restart:
ifeq ($(ENV),local)
	docker restart $(LOCAL_CONTAINER)
else
	$(SSH) $(SERVER_USER)@$(SERVER_HOST) "cd $(APP_BASE) && make restart"
endif
	@printf "$(GREEN)>>> 已重启$(NC)\n"

## 查看容器状态 [ENV=local|prod]
docker-status:
ifeq ($(ENV),local)
	@docker ps -a --filter name=$(LOCAL_CONTAINER)
else
	$(SSH) $(SERVER_USER)@$(SERVER_HOST) "cd $(APP_BASE) && make status"
endif

## 清理 [ENV=local 停删本地容器 | ENV=prod 仅删除本地镜像导出包]
docker-clean:
ifeq ($(ENV),local)
	@printf "$(YELLOW)>>> 停止并删除本地容器 $(LOCAL_CONTAINER)...$(NC)\n"
	-@docker stop $(LOCAL_CONTAINER) 2>/dev/null
	-@docker rm $(LOCAL_CONTAINER) 2>/dev/null
endif
	@rm -f $(IMAGE_TAR)
	@printf "$(GREEN)>>> 清理完成$(NC)\n"

## 测试生产服务器 SSH 连接
test-connection:
	@printf "$(YELLOW)>>> 测试连接 $(SERVER_USER)@$(SERVER_HOST)...$(NC)\n"
	$(SSH) $(SERVER_USER)@$(SERVER_HOST) "echo '>>> 连接成功' && uname -m && (docker --version || echo 'Docker 未安装, 请先执行 make server-init ENV=prod')"
