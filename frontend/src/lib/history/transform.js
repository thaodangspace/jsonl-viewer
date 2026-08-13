// transform.js — converts raw normalized WS-message-shaped events into
// messages-store-compatible view models.  Used by both batch history loading
// and live event rendering paths.
//
// Each incoming event is shaped like a WSMessage:
//   { type: "event", session_id: "...", data: { type, message?, ... }, time: "..." }

// ---------- internal helpers ----------

let _generateId;
function generateId() {
  if (!_generateId) {
    if (typeof crypto?.randomUUID === 'function') {
      _generateId = () => crypto.randomUUID();
    } else {
      _generateId = () =>
        'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
          const r = (Math.random() * 16) | 0;
          return (c === 'x' ? r : (r & 0x3) | 0x8).toString(16);
        });
    }
  }
  return _generateId();
}

function extractText(content) {
  if (typeof content === 'string') return content;
  if (Array.isArray(content)) {
    return content
      .filter(c => c.type === 'text')
      .map(c => (typeof c.text === 'string' ? c.text : String(c.text ?? '')))
      .join('');
  }
  return '';
}

function extractImages(content) {
  if (!Array.isArray(content)) return [];
  return content
    .filter(c => c.type === 'image')
    .map(c => ({ type: 'image', data: c.data, mimeType: c.mimeType || 'image/png' }));
}

// ---------- main APIs ----------

/**
 * Transform an array of raw event objects into messages-store-compatible view models.
 * Each event is shaped like:
 *   { type: "event", session_id, data: { type, id, message?, ... }, time }
 */
export function transformEventsToMessages(events) {
  const result = [];
  let currentAssistantId = null;

  for (const event of events) {
    const data = event.data || event;
    if (!data || !data.type) continue;

    switch (data.type) {
      case 'message_end': {
        const msg = data.message;
        if (!msg) break;
        if (msg.role === 'user') {
          const text = extractText(msg.content);
          const images = extractImages(msg.content);
          // Also check msg.images directly
          const allImages = images.length > 0 || (msg.images?.length > 0)
            ? [...images, ...(msg.images || [])]
            : [];
          if (text || allImages.length > 0) {
            const msgObj = {
              id: generateId(),
              role: 'user',
              content: text || '',
              timestamp: formatTime(data.timestamp || event.time),
            };
            if (allImages.length > 0) msgObj.images = allImages;
            result.push(msgObj);
          }
        } else if (msg.role === 'toolResult') {
          const toolResult = buildToolResult(msg);
          if (toolResult && !attachToolResult(result, toolResult)) {
            result.push(toolResult);
          }
        }
        break;
      }

      case 'message_update': {
        const ev = data.assistantMessageEvent;
        if (!ev) break;
        currentAssistantId = applyAssistantDelta(result, currentAssistantId, ev);
        break;
      }

      case 'message': {
        renderLegacyMessage(result, data);
        break;
      }
    }
  }

  // If an assistant was streaming but never got 'done', mark it as not streaming
  if (currentAssistantId) {
    for (let i = result.length - 1; i >= 0; i--) {
      if (result[i].id === currentAssistantId) {
        result[i] = { ...result[i], isStreaming: false };
        break;
      }
    }
  }

  return result;
}


// ---------- rendering helpers ----------

function formatTime(ts) {
  if (!ts) return '';
  try {
    return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  } catch {
    return '';
  }
}

// --- assistant streaming ---

function applyAssistantDelta(messages, currentId, ev) {
  if (ev.type === 'text_delta') {
    if (!currentId) {
      const id = generateId();
      messages.push({
        id,
        role: 'assistant',
        rawText: ev.delta,
        thinking: '',
        toolCalls: [],
        isStreaming: true,
        timestamp: formatTime(),
      });
      return id;
    }

    for (let i = messages.length - 1; i >= 0; i--) {
      if (messages[i].id === currentId) {
        messages[i] = { ...messages[i], rawText: (messages[i].rawText || '') + ev.delta, isStreaming: true };
        break;
      }
    }
    return currentId;
  }

  if (ev.type === 'thinking_delta') {
    for (let i = messages.length - 1; i >= 0; i--) {
      if (messages[i].id === currentId) {
        messages[i] = { ...messages[i], thinking: (messages[i].thinking || '') + ev.delta };
        break;
      }
    }
    return currentId;
  }

  if (ev.type === 'toolcall_start') {
    for (let i = messages.length - 1; i >= 0; i--) {
      if (messages[i].id === currentId) {
        messages[i] = {
          ...messages[i],
          toolCalls: [...(messages[i].toolCalls || []), {
            id: ev.toolCall?.id || '',
            name: ev.toolCall?.name || 'unknown',
            arguments: {},
          }],
        };
        break;
      }
    }
    return currentId;
  }

  if (ev.type === 'toolcall_end') {
    for (let i = messages.length - 1; i >= 0; i--) {
      if (messages[i].id === currentId) {
        const updatedCalls = (messages[i].toolCalls || []).map(tc =>
          tc.id === ev.toolCall?.id
            ? { ...tc, arguments: ev.toolCall?.arguments || {} }
            : tc
        );
        messages[i] = { ...messages[i], toolCalls: updatedCalls };
        break;
      }
    }
    return currentId;
  }

  if (ev.type === 'done') {
    for (let i = messages.length - 1; i >= 0; i--) {
      if (messages[i].id === currentId) {
        messages[i] = { ...messages[i], isStreaming: false };
        break;
      }
    }
    return null; // reset current assistant
  }

  return currentId;
}

// --- tool result ---

function buildToolResult(msg) {
  const toolCallId = msg.toolCallId || '';
  const toolName = msg.toolName || 'unknown';
  let content = '';
  if (msg.content) {
    if (typeof msg.content === 'string') content = msg.content;
    else if (Array.isArray(msg.content)) content = extractText(msg.content);
  }
  // Basic unescape (call site may do more)
  try {
    content = JSON.parse('"' + content.replace(/"/g, '\\"') + '"');
  } catch {}

  const filePath = msg.filePath || '';

  return {
    id: generateId(),
    role: 'toolResult',
    toolName,
    content: content || '(no output)',
    isError: msg.isError || false,
    toolCallId,
    filePath,
    language: null,
    timestamp: formatTime(),
  };
}

// Attach a result to its call so the call and output render as one component.
// Tool results normally arrive after the assistant message has completed, so
// search backwards rather than relying on the current assistant state.
function attachToolResult(messages, toolResult) {
  if (!toolResult.toolCallId) return false;

  for (let i = messages.length - 1; i >= 0; i--) {
    const message = messages[i];
    if (message.role !== 'assistant') continue;

    const toolCalls = message.toolCalls || [];
    if (!toolCalls.some(tc => tc.id === toolResult.toolCallId)) continue;

    messages[i] = {
      ...message,
      toolCalls: toolCalls.map(tc => tc.id === toolResult.toolCallId
        ? {
            ...tc,
            result: toolResult.content,
            resultIsError: toolResult.isError,
            resultFilePath: toolResult.filePath,
            resultLanguage: toolResult.language,
          }
        : tc),
    };
    return true;
  }

  return false;
}

// --- legacy message ---

function renderLegacyMessage(result, data) {
  const msg = data.message || {};
  const role = msg.role || 'unknown';
  if (role !== 'user' && role !== 'assistant' && role !== 'toolResult') return;

  const time = formatTime(data.timestamp);

  if (role === 'user') {
    const content = msg.content || [];
    const text = Array.isArray(content)
      ? content.filter(c => c.type === 'text').map(c => typeof c.text === 'string' ? c.text : String(c.text ?? '')).join('')
      : String(content);

    const images = Array.isArray(content)
      ? content.filter(c => c.type === 'image').map(c => ({ type: 'image', data: c.data, mimeType: c.mimeType || 'image/png' }))
      : [];

    if (text || images.length > 0) {
      const msgObj = { id: generateId(), role: 'user', content: text, timestamp: time };
      if (images.length > 0) msgObj.images = images;
      result.push(msgObj);
    }
  } else if (role === 'assistant') {
    const content = msg.content || [];
    let rawText = '';
    let thinking = '';
    const toolCalls = [];

    content.forEach(block => {
      if (block.type === 'text') {
        rawText += typeof block.text === 'string' ? block.text : String(block.text ?? '');
      } else if (block.type === 'thinking') {
        thinking += block.thinking || '';
      } else if (block.type === 'toolCall') {
        const name = block.toolCallName || block.name || 'unknown';
        const args = block.arguments || {};
        const toolId = block.id || '';
        toolCalls.push({ id: toolId, name, arguments: args });
      }
    });

    result.push({
      id: generateId(),
      role: 'assistant',
      rawText,
      thinking,
      toolCalls,
      isStreaming: false,
      timestamp: time,
    });
  } else if (role === 'toolResult') {
    const toolResult = buildToolResult(msg);
    if (toolResult && !attachToolResult(result, toolResult)) {
      result.push(toolResult);
    }
  }
}
