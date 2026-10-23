## GOFLY LIVE CHAT
Open-source live chat support system, built for modern customer service

​​Real-time messaging​​ - Instant connection between customers and support teams

Lightning-fast performance​​ - Powered by Golang for high-concurrency handling

### Technical Architecture​

A modern stack built for performance and scalability​
 
- Backend: `gin`, `jwt-go`, `websocket`, `go.uuid`, `gorm`, `cobra`  
- Frontend: `VueJS`, `ElementUI`  
- Database: `MySQL`  

---

### Installation & Usage  

#### 1. Set Up MySQL Database  
- Install and run MySQL (version ≥ 5.5).  
- Create a database:  
```sql
  CREATE DATABASE goflychat CHARSET utf8mb4;
 ```  
*  Configure Database Connection
   Edit mysql.json in the config directory:
```php
{
	"Server":"127.0.0.1",
	"Port":"3306",
	"Database":"goflychat",
	"Username":"goflychat",
	"Password":"goflychat"
}
```
* Install and Configure Golang
  Run the following commands:
```php
wget https://studygolang.com/dl/golang/go1.20.2.linux-amd64.tar.gz
tar -C /usr/local -xvf go1.20.2.linux-amd64.tar.gz
mv go1.20.2.linux-amd64.tar.gz /tmp
echo "PATH=\$PATH:/usr/local/go/bin" >> /etc/profile
echo "PATH=\$PATH:/usr/local/go/bin" >> ~/.bashrc
source /etc/profile
go version
go env -w GO111MODULE=on
go env -w GOPROXY=https://goproxy.cn,direct
```
* Download the Source Code

  Clone the repository in any directory:
```php
git clone https://github.com/taoshihan1991/goflylivechat.git
cd goflylivechat  
 ```  
* Initialize the Database
 ```php
 go run main.go install
 ```  
* Run the Application
```php
 go run main.go server
 ```
* ​​Build executable
```php
 go build -o gochat
```
* ​​Run binary​​:
```php
  Linux: ./gochat server (optional flags: -p 8082 -d)
  
  Windows: gochat.exe server (optional flags: -p 8082 -d)
```  
* Terminate the Process
```php
   killall gochat
``` 

Once running, the service listens on port 8081. Access via http://[your-ip]:8081.

For domain access, configure a reverse proxy to port 8081 to hide the port number.

### Docker deployment

Copy `.env.example` to `.env`, replace the passwords and session secret, then run:

```bash
docker compose up -d --build
```

The first startup imports `import.sql` automatically. Open `http://localhost:8081` after the app container is healthy. Set `GOFLY_ALLOWED_ORIGINS` to the exact website origins that embed the chat widget, separated by commas. Set `GOFLY_SESSION_SECRET` and `GOFLY_JWT_SECRET` to long random values in production.

MariaDB is also available on host port `3306` for external database clients. Connect to the Docker host IP with database `goflychat`, user `goflychat`, and the password from `GOFLY_DB_PASSWORD` in `.env`. Public internet access also requires TCP port `3306` to be reachable through the host firewall and any upstream router or cloud security group. Use a strong database password before allowing public access.

The database, installation lock, and uploaded files are stored in named Docker volumes. To reset the demo database and files, run `docker compose down -v`.

The Dockerfile uses DaoCloud's domestic Docker Hub mirror, Aliyun's Alpine mirror, and `goproxy.cn` for Go dependencies. If a mirror is temporarily unavailable, replace the `m.daocloud.io` prefix with another reachable domestic mirror.

### 一键安装到 Linux 服务器

在本机运行，复用当前 Dockerfile 和 Compose。本机需启动 Docker（含 Buildx），服务器需可通过 root SSH 登录；自动安装 Docker 支持 Debian/Ubuntu，已有 Docker 和 Compose >= 2.24.4 的其他 Linux 也可使用。支持 x86_64 和 ARM64。

```bash
bash scripts/deploy-linux.sh
```

首次运行按提示输入服务器地址、SSH 端口（默认 22）、用户（默认 root）及可选私钥路径。SSH 连通后，脚本自动创建本机 `scripts/deploy.env` 并设置权限 600；以后直接运行同一条命令。登录密码由 SSH 提示输入，不保存；使用密钥或 ssh-agent 时可免密登录。配置文件不会进入 Git 或 Docker 构建上下文。

脚本会安装 Docker、生成独立生产密钥、本机构建并上传应用镜像、启动 MariaDB、首次初始化并替换默认客服密码，最后等待后台页面健康检查。自动化环境也可通过 `DEPLOY_HOST` 等环境变量配置；`SSH_PASSWORD` 环境变量模式需要本机安装 `sshpass`。高级配置项见 `scripts/deploy.env.example`，无需复制模板。

默认目录 `/opt/goflylivechat`，后台 `http://服务器IP:8081/login`，账号 `agent`，初始随机密码在部署输出以及服务器的 `initial-admin-password.txt` 中。云安全组需放行 TCP 8081（修改端口时相应调整）；数据库不发布到宿主机端口。远端需能访问 Docker 官方软件源及 Compose 中的 MariaDB 镜像地址。HTTPS 和域名反代需另行配置。

重复运行同一条命令即可更新应用，保留远端 `.env`、密码、数据库、安装锁、附件和头像；不会上传本机开发 `.env`。后续更改端口、允许来源或 App 接入配置，请修改服务器 `.env` 后重跑。更新会重建应用容器，已有聊天连接可能短暂中断；更新前请自行备份数据库和数据卷。脚本不执行数据删除或自动数据库回滚。

远端排查命令（按实际部署目录调整）：

```bash
cd /opt/goflylivechat
docker compose -p goflylivechat -f docker-compose.yml -f compose.production.yml ps
docker compose -p goflylivechat -f docker-compose.yml -f compose.production.yml logs --tail=100 app db
```

如检测到已有数据却缺少 `.env` 或安装锁，脚本会停止，避免重新生成密码或执行会删除旧表的 `import.sql`。请恢复原配置/安装锁再重跑，不要删除数据卷。当前一台服务器使用固定 Compose 项目名 `goflylivechat`，不支持同机多实例。

离线验证：`python3 -m unittest discover -s tests -p 'deploy_linux_test.py' -v`。

### Unread messages

The online and recent visitor lists show unread incoming message counts; the Chat menu shows their total, including offline visitors. Counts are stored in the database and restored after refreshing or signing in again. Opening a conversation marks incoming messages through its newest displayed message ID as read. Messages received while the chat is hidden stay unread. Existing messages whose database status is still `unread` are included in the initial counts.

With a customer service token, `GET /message_unread` returns `result.counts` keyed by visitor ID and `result.total`. `GET /message_status?visitor_id=...` returns the authenticated recipient's incoming unread count. `POST /message_status` requires `visitor_id` and a positive `last_message_id`; it marks only incoming messages through that ID as read. Both message-status endpoints also accept the visitor's own token, with the direction reversed, and enforce conversation ownership.

### Customer Service Integration
Chat Link

http://127.0.0.1:8081/livechat?user_id=agent

Popup Integration

```
<script>
    (function(global, document, scriptUrl, callback) {
        const head = document.getElementsByTagName('head')[0];
        const script = document.createElement('script');
        script.type = 'text/javascript';
        script.src = scriptUrl + "/static/js/chat-widget.js";
        script.onload = script.onreadystatechange = function () {
            if (!this.readyState || this.readyState === "loaded" || this.readyState === "complete") {
                callback(scriptUrl);
            }
        };
        head.appendChild(script);
    })(window, document, "http://127.0.0.1:8081", function(baseUrl) {
        CHAT_WIDGET.initialize({
            API_URL: baseUrl,
            AGENT_ID: "agent",
        });
    });
</script>
```
### Important Notice  
The use of this project for illegal or non-compliant purposes, including but not limited to viruses, trojans, pornography, gambling, fraud, prohibited items, counterfeit products, false information, cryptocurrencies, and financial violations, is strictly prohibited.  

This project is intended solely for personal learning and testing purposes. Any commercial use or illegal activities are explicitly forbidden!!!  



### Copyright Notice
This project provides full-featured code but is intended ​​only for personal demonstration and testing​​. Commercial use is strictly prohibited.

By using this software, you agree to comply with all applicable local laws and regulations. ​​You are solely responsible for any legal consequences arising from misuse.​
