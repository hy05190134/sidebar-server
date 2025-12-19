//wecom-sidebar.js
class WeComSidebarAssistant {
  constructor() {
    this.agentId = null;
    this.chatId = null;
    this.autoAI = false;
    this.websocket = null;
    this.isConnected = false;
    
    this.init();
  }
  
  async init() {
    // 1. 初始化企业微信SDK
    await this.initWeComSDK();
    
    // 2. 获取当前上下文
    await this.getContext();
    
    // 3. 连接WebSocket
    await this.connectWebSocket();
    
    // 4. 设置事件监听器
    this.setupEventListeners();
  }

  /**
   * 供 ww.register 使用的 config 签名生成函数
   * @param {string} url - 当前页面的完整URL（用于生成签名）
   * @returns {Promise<Object>} 返回一个Promise，解析为签名对象
   */
  getConfigSignature = (url) => {
    // 1. 从参数中获取当前页面的URL（SDK会自动传入）
    const currentUrl = url;
    console.log('[签名函数] 接收到URL:', currentUrl);

    // 2. 调用你已有的后端签名接口
    // 注意：URL需要先移除hash部分，并且encodeURIComponent
    const apiUrl = `http://111.230.112.121:8080/api/wx-config?url=${encodeURIComponent(currentUrl.split('#')[0])}`;

    // 3. 返回一个Promise，SDK会等待其完成
    return fetch(apiUrl)
      .then(response => {
        if (!response.ok) {
          throw new Error(`网络响应错误: ${response.status}`);
        }
        return response.json();
      })
      .then(configData => {
        // 4. 确保返回的对象格式符合SDK要求
        console.log('[签名函数] 从后端获取到配置:', {
          timestamp: configData.timestamp,
          nonceStr: configData.nonceStr,
          signaturePreview: configData.signature ? `${configData.signature.substring(0, 10)}...` : '空'
        });

        // 返回的结构必须包含 timestamp, nonceStr, signature
        return {
          timestamp: configData.timestamp,   // 可以是字符串或数字
          nonceStr: configData.nonceStr,
          signature: configData.signature
        };
      })
      .catch(error => {
        console.error('[签名函数] 获取签名失败:', error);
        // 5. 重要：即使失败，也必须返回一个符合格式的对象，否则SDK会报错
        // 这里返回一个模拟签名（仅用于开发测试，生产环境应处理错误）
        const fallbackTimestamp = Math.floor(Date.now() / 1000);
        const fallbackNonceStr = 'fallback_nonce_' + Date.now();
        return {
          timestamp: fallbackTimestamp,
          nonceStr: fallbackNonceStr,
          signature: 'mock_signature_for_debug_' + fallbackTimestamp,
          isMock: true // 自定义标记，便于识别
        };
      });
  }

  async initWeComSDK() {
    ww.register({
      corpId: 'ww472d8d6f6c16bd79',
      agentId: '1000002', 
      jsApiList: [
        'sendChatMessage',
        'getContext',
        'onChatMessage',
        'openEnterpriseChat',
        'getExternalContact',
        'showModal'
      ],
      getConfigSignature: this.getConfigSignature
    }) 
    
    //const response = await fetch(`http://111.230.112.121:8080/api/wx-config?url=${window.location.href}`);
    //const config = await response.json();
    //return new Promise((resolve) => {
    //  if (typeof wx !== 'undefined') {
    //    wx.config({
    //      // 企业微信配置参数（需要从后端获取）
    //      beta: true,
    //      debug: true,
    //      appId: config.corpId, // 企业的CorpID
    //      timestamp: config.timestamp,
    //      nonceStr: config.nonceStr,
    //      signature: config.signature,
    //      jsApiList: [
    //        'sendChatMessage',
    //        'getContext',
    //        'onChatMessage',
    //        'openEnterpriseChat'
    //      ]
    //    });
    //    
    //    wx.ready(() => {
    //      console.log('企业微信JS-SDK初始化完成');
    //      resolve();
    //    });
    //    
    //    wx.error((err) => {
    //      console.error('企业微信JS-SDK初始化失败:', err);
    //      resolve(); // 继续执行，使用备用方案
    //    });
    //  } else {
    //    console.warn('企业微信JS-SDK未加载，使用测试模式');
    //    resolve();
    //  }
    //});
  }

  async getContext() {
    this.agentId = '1000002';

    // 1. 先确定入口（判断是否在侧边栏）
    if (typeof ww !== 'undefined' && ww.getContext) {
      const that = this;
      ww.getContext({
        success(res) {
          console.log('进入场景:', res.entry);
        },
        fail(err) {
          // 目前因为网站域名没有备案，没有可信域名，等备案后就可以添加
          //that.chatId = err.errMsg;
        }
      });        
    }

    // 2. 关键步骤：尝试获取外部联系人ID（侧边栏核心场景）
    if (typeof ww !== 'undefined' && ww.getCurExternalContact) {
      const that = this;
      try {
        const externalRes = await new Promise((resolve, reject) => {
          ww.getCurExternalContact({
            success: resolve,
            fail: reject
          });
        });

        // 调用成功，说明当前确实在外部单聊侧边栏
        if (externalRes.userId) {
          console.log('[上下文] 获取到外部客户ID:', externalRes.userId);

          // 此时可以确定场景，设置agentId和chatId供其他逻辑使用
          that.chatId = externalRes.userId;
        }
      } catch (externalError) {
        // 获取失败，说明不在外部单聊工具栏，可能是其他入口或内部聊天
        that.chatId = externalError.errMsg;
        console.warn('[上下文] 未在外部聊天侧边栏，或权限不足:', externalError.errMsg || externalError);
      }
    }

    this.chatId = 'wm1fsMCAAAkQY9cI0nhkZ6qAbM3NmZUQ'; 
  }

  async connectWebSocket() {
    try {
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const wsUrl = `${protocol}//${window.location.host}:8080/ws/wecom`;
      
      this.websocket = new WebSocket(wsUrl);
      
      this.websocket.onopen = () => {
        console.log('WebSocket连接成功');
        this.isConnected = true;
        this.updateStatus('connected');
        
        // 发送认证信息
        this.sendToServer({
          type: 'auth',
          agent_id: this.agentId,
          chat_id: this.chatId
        });
      };
      
      this.websocket.onmessage = (event) => {
        const data = JSON.parse(event.data);
        this.handleServerMessage(data);
      };
      
      this.websocket.onclose = () => {
        console.log('WebSocket连接关闭');
        this.isConnected = false;
        this.updateStatus('disconnected');
        
        // 5秒后重连
        setTimeout(() => this.connectWebSocket(), 5000);
      };
      
      this.websocket.onerror = (error) => {
        console.error('WebSocket错误:', error);
      };
      
    } catch (error) {
      console.error('连接WebSocket失败:', error);
    }
  }
  
  async sendWeComMessage(content, msgtype = 'text') {
    // 在企业微信侧边栏中发送消息到主聊天窗口
    try {
      const message = {
        msgtype: msgtype
      };
      
      switch (msgtype) {
        case 'text':
          message.text = { content: content };
          break;
        case 'image':
          message.image = { media_id: content };
          break;
        case 'file':
          message.file = { media_id: content };
          break;
        case 'video':
          // video 类型：content 应为对象，包含 media_id，可选 thumb_media_id
          if (typeof content === 'string') {
            message.video = { media_id: content };
          } else {
            message.video = {
              media_id: content.media_id,
              thumb_media_id: content.thumb_media_id || content.thumbMediaId
            };
          }
          break;
        case 'miniprogram':
          // miniprogram 类型：content 应为对象，包含 appid, pagepath, title，可选 thumb_media_id
          if (typeof content === 'string') {
            throw new Error('miniprogram 类型消息需要对象格式，包含 appid, pagepath, title');
          } else {
            message.miniprogram = {
              appid: content.appid,
              pagepath: content.pagepath,
              title: content.title,
              thumb_media_id: content.thumb_media_id || content.thumbMediaId
            };
          }
          break;
        case 'news':
          // news 类型：content 应为对象，包含 articles 数组
          if (typeof content === 'string') {
            // 如果传入字符串，尝试解析为 JSON
            try {
              const parsed = JSON.parse(content);
              message.news = { articles: parsed.articles || parsed };
            } catch (e) {
              throw new Error('news 类型消息需要 articles 数组');
            }
          } else {
            message.news = {
              articles: content.articles || (Array.isArray(content) ? content : [content])
            };
          }
          break;
      }
      
      console.log('准备发送企业微信消息:', message);
      
      if (typeof ww !== 'undefined' && ww.sendChatMessage) {
        const result = await ww.sendChatMessage(message);
        console.log('sendChatMessage返回:', result);

        if (result.err_msg === 'sendChatMessage:ok') {
          // 发送成功，通知服务器
          this.sendMessageToServer({
            type: 'agent_message_sent',
            msg_id: result.msgId || `msg_${Date.now()}`,
            content: content,
            msgtype: msgtype,
            chat_id: this.chatId,
            agent_id: this.agentId,
            timestamp: Date.now()
          });

          return result;
        } else {
          console.error('发送消息失败:', result);
          throw new Error(result.err_msg);
        }
      } else {
        // 开发环境模拟发送
        console.log('[模拟]发送企业微信消息:', content);
        const mockResult = {
          err_msg: 'sendChatMessage:ok',
          msgId: `mock_msg_${Date.now()}`
        };

        // 模拟发送到服务器
        this.sendMessageToServer({
          type: 'agent_message_sent',
          msg_id: mockResult.msgId,
          content: content,
          msgtype: msgtype,
          chat_id: this.chatId,
          agent_id: this.agentId,
          timestamp: Date.now()
        });

        return mockResult;
      }
    } catch (error) {
      console.error('发送消息失败:', error);
      throw error;
    }
  }
  
  handleServerMessage(data) {
    switch (data.type) {
      case 'ai_suggestion':
        this.displayAISuggestion(data);
        break;
      case 'customer_message':
        if (this.autoAI) {
          // 自动触发AI分析
          this.requestAIAssistance(data);
        }
        break;
      case 'heartbeat':
        this.sendToServer({ type: 'pong' });
        break;
      case 'auth_success':
        console.log('认证成功');
        break;
      case 'poll_interval_updated':
        this.handlePollIntervalUpdated(data);
        break;
      case 'poll_interval_info':
        this.handlePollIntervalInfo(data);
        break;
      case 'poll_interval_error':
        this.handlePollIntervalError(data);
        break;
    }
  }
  
  displayAISuggestion(data) {
    const suggestionId = data.suggestion_id || `suggestion_${Date.now()}`;
    
    const suggestionHTML = `
      <div class="ai-suggestion" data-suggestion-id="${suggestionId}">
        <div class="suggestion-text">
          <strong>🤖 AI建议：</strong>
          <p>${data.text}</p>
          <small>置信度: ${(data.confidence * 100).toFixed(1)}%</small>
        </div>
        <div class="suggestion-actions">
          <button class="action-btn primary" onclick="sideBarAssistant.useSuggestion('${suggestionId}')">
            发送此建议
          </button>
          <button class="action-btn" onclick="sideBarAssistant.editSuggestion('${suggestionId}')">
            编辑后发送
          </button>
          <button class="action-btn" onclick="sideBarAssistant.rejectSuggestion('${suggestionId}')">
            不采用
          </button>
        </div>
      </div>
    `;
    
    const container = document.getElementById('suggestionsContainer');
    container.insertAdjacentHTML('afterbegin', suggestionHTML);
    
    // 限制显示数量
    const suggestions = container.querySelectorAll('.ai-suggestion');
    if (suggestions.length > 5) {
      suggestions[suggestions.length - 1].remove();
    }
  }
  
  useSuggestion(suggestionId) {
    const suggestionElement = document.querySelector(`[data-suggestion-id="${suggestionId}"]`);
    if (!suggestionElement) return;
    
    const textElement = suggestionElement.querySelector('.suggestion-text p');
    const text = textElement?.textContent || '';
    
    if (text.trim()) {
      // 1. 发送消息
      this.sendWeComMessage(text, 'text');
      
      // 2. 发送反馈到服务器
      this.sendToServer({
        type: 'ai_feedback',
        suggestion_id: suggestionId,
        action: 'used',
        content: text
      });
      
      // 3. 标记为已使用
      suggestionElement.style.opacity = '0.6';
      suggestionElement.querySelectorAll('button').forEach(btn => {
        btn.disabled = true;
      });
      
      // 4. 3秒后移除
      setTimeout(() => {
        suggestionElement.remove();
      }, 3000);
    }
  }
  
  editSuggestion(suggestionId) {
    const suggestionElement = document.querySelector(`[data-suggestion-id="${suggestionId}"]`);
    if (!suggestionElement) return;
    
    const textElement = suggestionElement.querySelector('.suggestion-text p');
    const originalText = textElement?.textContent || '';
    
    // 直接显示编辑输入框（完全替代 prompt）
    this.showEditInputModal(suggestionId, originalText, textElement, suggestionElement);
  }

  // 显示自定义编辑输入框（模拟 prompt 样式）
  showEditInputModal(suggestionId, originalText, textElement, suggestionElement) {
    // 创建模态框遮罩层
    const modalOverlay = document.createElement('div');
    modalOverlay.style.cssText = `
      position: fixed;
      top: 0;
      left: 0;
      right: 0;
      bottom: 0;
      background: rgba(0, 0, 0, 0.3);
      z-index: 10000;
      display: flex;
      align-items: center;
      justify-content: center;
    `;

    // 创建模态框内容（模拟 prompt 样式）
    const modalContent = document.createElement('div');
    modalContent.style.cssText = `
      background: #fff;
      border: 1px solid #ccc;
      width: 400px;
      padding: 10px;
      box-shadow: 0 2px 8px rgba(0, 0, 0, 0.2);
    `;

    const title = document.createElement('div');
    title.textContent = '编辑AI建议';
    title.style.cssText = `
      margin-bottom: 10px;
      font-size: 13px;
    `;

    const textarea = document.createElement('textarea');
    textarea.value = originalText;
    textarea.style.cssText = `
      width: 100%;
      height: 80px;
      padding: 4px;
      border: 1px solid #ccc;
      font-size: 13px;
      font-family: inherit;
      box-sizing: border-box;
      margin-bottom: 10px;
      resize: none;
    `;
    textarea.focus();
    textarea.select();

    const buttonContainer = document.createElement('div');
    buttonContainer.style.cssText = `
      text-align: right;
    `;

    const cancelBtn = document.createElement('button');
    cancelBtn.textContent = '取消';
    cancelBtn.style.cssText = `
      padding: 4px 12px;
      margin-right: 8px;
      border: 1px solid #ccc;
      background: #fff;
      cursor: pointer;
      font-size: 13px;
    `;
    cancelBtn.onclick = () => {
      document.body.removeChild(modalOverlay);
    };

    const confirmBtn = document.createElement('button');
    confirmBtn.textContent = '确定';
    confirmBtn.style.cssText = `
      padding: 4px 12px;
      border: 1px solid #ccc;
      background: #fff;
      cursor: pointer;
      font-size: 13px;
    `;
    confirmBtn.onclick = () => {
      const editedText = textarea.value.trim();
      if (editedText && editedText !== originalText) {
        // 1. 发送编辑后的消息
        this.sendWeComMessage(editedText, 'text');

        // 2. 发送反馈到服务器
        this.sendToServer({
          type: 'ai_feedback',
          suggestion_id: suggestionId,
          action: 'edited',
          original_content: originalText,
          edited_content: editedText
        });

        // 3. 更新显示
        textElement.textContent = editedText;
        suggestionElement.style.borderColor = '#1890ff';

        // 4. 禁用所有按钮，防止重复发送
        const allButtons = suggestionElement.querySelectorAll('button');
        allButtons.forEach(btn => {
          btn.disabled = true;
          btn.style.opacity = '0.5';
          btn.style.cursor = 'not-allowed';
        });

        // 5. 3秒后移除
        setTimeout(() => {
          suggestionElement.remove();
        }, 3000);
      }
      document.body.removeChild(modalOverlay);
    };

    // ESC 键关闭
    const handleEsc = (e) => {
      if (e.key === 'Escape') {
        document.body.removeChild(modalOverlay);
        document.removeEventListener('keydown', handleEsc);
      }
    };
    document.addEventListener('keydown', handleEsc);

    buttonContainer.appendChild(cancelBtn);
    buttonContainer.appendChild(confirmBtn);
    modalContent.appendChild(title);
    modalContent.appendChild(textarea);
    modalContent.appendChild(buttonContainer);
    modalOverlay.appendChild(modalContent);
    document.body.appendChild(modalOverlay);
  }

  rejectSuggestion(suggestionId) {
    const suggestionElement = document.querySelector(`[data-suggestion-id="${suggestionId}"]`);
    
    // 发送反馈到服务器
    this.sendToServer({
      type: 'ai_feedback',
      suggestion_id: suggestionId,
      action: 'rejected'
    });
    
    // 淡出移除
    if (suggestionElement) {
      suggestionElement.style.transition = 'opacity 0.3s';
      suggestionElement.style.opacity = '0';
      setTimeout(() => {
        suggestionElement.remove();
      }, 300);
    }
  }
  
  async requestAIAssistance(messageData = null) {
    // 向服务器请求AI协助
    const requestData = {
      type: 'ai_assistance_request',
      agent_id: this.agentId,
      chat_id: this.chatId,
      timestamp: Date.now()
    };
    
    if (messageData) {
      requestData.content = messageData;
    } else {
      // 如果没有提供消息数据，尝试获取最近的消息
      requestData.content = await this.getRecentMessages();
    }
    
    this.sendToServer(requestData);
  }
  
  sendToServer(data) {
    if (this.websocket && this.isConnected) {
      this.websocket.send(JSON.stringify(data));
    } else {
      console.warn('WebSocket未连接，无法发送数据:', data);
      // 可以存储到localStorage，等连接恢复后发送
      this.queueMessage(data);
    }
  }
  
  queueMessage(data) {
    const queue = JSON.parse(localStorage.getItem('wecom_msg_queue') || '[]');
    queue.push({
      data: data,
      timestamp: Date.now()
    });
    
    // 只保留最近100条
    if (queue.length > 100) {
      queue.shift();
    }
    
    localStorage.setItem('wecom_msg_queue', JSON.stringify(queue));
  }
  
  retryQueuedMessages() {
    const queue = JSON.parse(localStorage.getItem('wecom_msg_queue') || '[]');
    
    for (const item of queue) {
      this.sendToServer(item.data);
    }
    
    // 清空队列
    localStorage.removeItem('wecom_msg_queue');
  }
  
  updateStatus(status) {
    const statusDot = document.getElementById('statusDot');
    const statusText = document.getElementById('statusText');
    
    switch (status) {
      case 'connected':
        statusDot.className = 'status-dot connected';
        statusText.textContent = '已连接';
        break;
      case 'disconnected':
        statusDot.className = 'status-dot';
        statusText.textContent = '连接断开';
        break;
      case 'connecting':
        statusDot.className = 'status-dot';
        statusText.textContent = '连接中...';
        break;
    }
  }
  
  setupEventListeners() {
    // 监听可见性变化，当侧边栏显示时重连
    document.addEventListener('visibilitychange', () => {
      if (!document.hidden && !this.isConnected) {
        this.connectWebSocket();
      }
    });
  }

  /**
   * 设置轮询间隔
   * @param {number} interval - 间隔时间（秒）
   */
  setPollInterval(interval) {
    if (interval < 1 || interval > 3600) {
      console.error('轮询间隔必须在 1-3600 秒之间');
      return;
    }

    this.sendToServer({
      type: 'set_poll_interval',
      agent_id: this.agentId,
      content: {
        interval: interval
      }
    });
  }

  /**
   * 获取当前轮询间隔
   */
  getPollInterval() {
    this.sendToServer({
      type: 'get_poll_interval',
      agent_id: this.agentId
    });
  }

  /**
   * 处理轮询间隔更新响应
   */
  handlePollIntervalUpdated(data) {
    console.log('轮询间隔已更新:', data.poll_interval, '秒');
    if (data.note) {
      console.log('提示:', data.note);
    }
    // 可以更新UI显示
    this.updatePollIntervalDisplay(data.poll_interval);
  }

  /**
   * 处理轮询间隔信息响应
   */
  handlePollIntervalInfo(data) {
    console.log('当前轮询间隔:', data.poll_interval, '秒');
    console.log('轮询状态:', data.is_polling ? '运行中' : '未运行');
    // 更新UI显示
    this.updatePollIntervalDisplay(data.poll_interval, data.is_polling);
  }

  /**
   * 处理轮询间隔错误响应
   */
  handlePollIntervalError(data) {
    console.error('轮询间隔设置失败:', data.error);
    // 可以显示错误提示给用户
    alert(`轮询间隔设置失败: ${data.error}`);
  }

  /**
   * 更新轮询间隔显示（如果UI中有相关元素）
   */
  updatePollIntervalDisplay(interval, isPolling = null) {
    const displayElement = document.getElementById('pollIntervalDisplay');
    if (displayElement) {
      displayElement.textContent = `${interval} 秒`;
      if (isPolling !== null) {
        const statusElement = document.getElementById('pollStatusDisplay');
        if (statusElement) {
          statusElement.textContent = isPolling ? '运行中' : '未运行';
          statusElement.style.color = isPolling ? '#10b981' : '#6b7280';
        }
      }
    }
  }
}

// 全局访问
window.sideBarAssistant = new WeComSidebarAssistant();

// 全局函数供HTML调用
window.requestAIHelp = function() {
  sideBarAssistant.requestAIAssistance("this is test for send into chat");
};

window.toggleAutoAI = function() {
  sideBarAssistant.autoAI = !sideBarAssistant.autoAI;
  const statusElement = document.getElementById('autoAIStatus');
  statusElement.textContent = sideBarAssistant.autoAI ? '开启' : '关闭';
  statusElement.style.color = sideBarAssistant.autoAI ? '#10b981' : '#6b7280';
};

// 设置轮询间隔（全局函数）
window.setPollInterval = function(interval) {
  sideBarAssistant.setPollInterval(interval);
};

// 获取轮询间隔（全局函数）
window.getPollInterval = function() {
  sideBarAssistant.getPollInterval();
};
