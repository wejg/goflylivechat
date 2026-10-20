const CHAT_WIDGET = {
    API_URL: "",
    AGENT_ID: "",
    AUTO_OPEN: true,
    DISPLAY_MODE: 1,
    USER_ID: "",
    USER_NAME: "",
    USER_AVATAR: "",
    isChatOpen: false,
    originalPageTitle: document.title,
    chatWindowTitle: "Chat with us",
    isOffline: false,
    iframeId: "chat-widget-iframe",
    containerId: "chat-widget-container",
    titleFlashTimer: null
};

CHAT_WIDGET.initialize = function(config) {
    // Apply configuration
    for (let key in config) {
        if (this.hasOwnProperty(key)) {
            this[key] = config[key];
        }
    }

    // Normalize URL by removing trailing slash
    if (this.API_URL) {
        this.API_URL = this.API_URL.replace(/\/$/, "");
    }

    // Add required CSS styles
    this.injectStyles();

    // Display the chat button
    this.createChatButton();

    // Set up event handlers
    this.setupEventHandlers();
};

CHAT_WIDGET.injectStyles = function() {
    const style = document.createElement('style');
    style.textContent = `
        #chat-widget-button {
            position: fixed;
            bottom: 20px;
            right: 20px;
            width: 60px;
            height: 60px;
            background-color: #1E88E5;
            color: white;
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            cursor: pointer;
            box-shadow: 0 2px 10px rgba(0,0,0,0.2);
            z-index: 9999;
        }
        
        #chat-widget-button .notification-badge {
            position: absolute;
            top: -5px;
            right: -5px;
            background-color: #FF5722;
            color: white;
            border-radius: 50%;
            width: 20px;
            height: 20px;
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 12px;
        }
        
        #chat-widget-container {
            position: fixed;
            bottom: 90px;
            right: 20px;
            width: 350px;
            height: 500px;
            background: white;
            border-radius: 8px;
            box-shadow: 0 5px 20px rgba(0,0,0,0.2);
            display: none;
            flex-direction: column;
            z-index: 9998;
            overflow: hidden;
        }
        
        #chat-widget-header {
            padding: 15px;
            background: #1E88E5;
            color: white;
            display: flex;
            align-items: center;
        }
        
        #chat-widget-iframe {
            flex: 1;
            border: none;
        }
        
        .close-button {
            margin-left: auto;
            cursor: pointer;
            font-size: 20px;
        }
        @media (max-width: 800px) {
          #chat-widget-container {
            width: 100% !important;
            right: 0 !important;
            bottom: 80px !important;
          }
        }
    `;
    document.head.appendChild(style);
};

CHAT_WIDGET.createChatButton = function() {
    const button = document.createElement('div');
    button.id = 'chat-widget-button';
    button.innerHTML = `
        <svg width="24" height="24" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
            <path d="M21 15C21 15.5304 20.7893 16.0391 20.4142 16.4142C20.0391 16.7893 19.5304 17 19 17H7L3 21V5C3 4.46957 3.21071 3.96086 3.58579 3.58579C3.96086 3.21071 4.46957 3 5 3H19C19.5304 3 20.0391 3.21071 20.4142 3.58579C20.7893 3.96086 21 4.46957 21 5V15Z" fill="white"/>
        </svg>
        <div class="notification-badge" style="display: none;">0</div>
    `;
    document.body.appendChild(button);

    button.addEventListener('click', () => {
        this.openChatWindow();
    });

    // Open automatically if configured
    if (this.AUTO_OPEN) {
        setTimeout(() => {
            this.openChatWindow();
        }, 3000);
    }
};

CHAT_WIDGET.openChatWindow = function() {
    if (this.isChatOpen) return;

    const badge = document.querySelector('#chat-widget-button .notification-badge');
    badge.style.display = 'none';
    badge.textContent = '0';

    // Create container if it doesn't exist
    if (!document.getElementById(this.containerId)) {
        const container = document.createElement('div');
        container.id = this.containerId;
        container.innerHTML = `
            <div id="chat-widget-header">
                <span>在线客服</span>
                <span class="close-button">×</span>
            </div>
            <iframe id="${this.iframeId}" src="${this.buildChatUrl()}"></iframe>
        `;
        document.body.appendChild(container);

        // Add close button handler
        document.querySelector(`#${this.containerId} .close-button`).addEventListener('click', () => {
            this.closeChatWindow();
        });
    }

    // Show the chat window
    document.getElementById(this.containerId).style.display = 'flex';
    this.isChatOpen = true;

    // Hide the floating button
    document.getElementById('chat-widget-button').style.display = 'none';
};

CHAT_WIDGET.closeChatWindow = function() {
    const container = document.getElementById(this.containerId);
    if (container) {
        // 移除 iframe 而不是只隐藏：隐藏的 iframe 会继续跑 WebSocket 和心跳定时器。
        container.remove();
    }
    this.isChatOpen = false;
    if (this.titleFlashTimer) {
        clearInterval(this.titleFlashTimer);
        this.titleFlashTimer = null;
        document.title = this.originalPageTitle;
    }
    document.getElementById('chat-widget-button').style.display = 'flex';
};

CHAT_WIDGET.buildChatUrl = function() {
    let url = `${this.API_URL}/livechat?user_id=${encodeURIComponent(this.AGENT_ID)}`;

    if (this.USER_ID) {
        url += `&visitor_id=${encodeURIComponent(this.USER_ID)}`;
    }
    if (this.USER_NAME) {
        url += `&visitor_name=${encodeURIComponent(this.USER_NAME)}`;
    }
    if (this.USER_AVATAR) {
        url += `&visitor_avatar=${encodeURIComponent(this.USER_AVATAR)}`;
    }

    return url;
};

CHAT_WIDGET.setupEventHandlers = function() {
    // Handle messages from the chat iframe
    window.addEventListener('message', (e) => {
        if (!e.data || !e.data.type) return;
        const iframe = document.getElementById(this.iframeId);
        let expectedOrigin;
        try {
            expectedOrigin = new URL(this.API_URL, window.location.href).origin;
        } catch (_) {
            return;
        }
        if (e.origin !== expectedOrigin || !iframe || e.source !== iframe.contentWindow) return;

        switch (e.data.type) {
            case 'new_message':
                this.handleIncomingMessage(e.data);
                break;
            case 'close_chat':
                this.closeChatWindow();
                break;
        }
    });
};

CHAT_WIDGET.handleIncomingMessage = function(data) {
    // Update notification badge
    const badge = document.querySelector('#chat-widget-button .notification-badge');
    let count = parseInt(badge.textContent || '0');

    if (!this.isChatOpen) {
        count++;
        badge.textContent = count;
        badge.style.display = 'flex';

        // Flash title if window is not focused
        if (!document.hasFocus()) {
            this.notifyWithTitleFlash();
        }
    }
};

CHAT_WIDGET.notifyWithTitleFlash = function() {
    if (this.titleFlashTimer) return;
    let isFlashing = true;
    this.titleFlashTimer = setInterval(() => {
        document.title = isFlashing ? "新消息" : this.originalPageTitle;
        isFlashing = !isFlashing;
    }, 1000);

    window.addEventListener('focus', () => {
        clearInterval(this.titleFlashTimer);
        this.titleFlashTimer = null;
        document.title = this.originalPageTitle;
    }, { once: true });
};