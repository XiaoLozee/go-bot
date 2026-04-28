export interface DebugParamDefinition {
  key: string
  label: string
  type: string
  required: boolean
  description: string
  input: 'text' | 'json'
}

export interface DebugMethodDefinition {
  method: string
  category: string
  label: string
  summary: string
  hint: string
  params: DebugParamDefinition[]
  template: (connectionID: string) => Record<string, unknown>
  exampleResponse: Record<string, unknown>
}

const targetTemplate = (connectionID: string) => ({
  connection_id: connectionID,
  chat_type: 'private',
  user_id: '',
  group_id: '',
})

export const debugMethodCatalog: DebugMethodDefinition[] = [
  {
    method: 'messenger.send_text',
    category: '消息接口',
    label: '发送文本消息',
    summary: '通过 env.messenger 向私聊或群聊目标发送纯文本。',
    hint: '私聊必须填写 user_id，群聊必须填写 group_id。',
    params: [
      { key: 'target', label: '消息目标', type: 'object', required: true, description: '消息目标对象。', input: 'json' },
      { key: 'text', label: '消息文本', type: 'string', required: true, description: '要发送的文本内容。', input: 'text' },
    ],
    template: (connectionID) => ({ target: targetTemplate(connectionID), text: 'hello from plugin api debug' }),
    exampleResponse: { accepted: true, method: 'messenger.send_text', result: { sent: true }, message: '接口调用成功' },
  },
  {
    method: 'messenger.reply_text',
    category: '消息接口',
    label: '回复文本消息',
    summary: '通过 env.messenger 对指定消息发送文本回复。',
    hint: 'reply_to 通常来自事件里的 message id。',
    params: [
      { key: 'target', label: '消息目标', type: 'object', required: true, description: '消息目标对象。', input: 'json' },
      { key: 'reply_to', label: '回复消息 ID', type: 'string | number', required: true, description: '要回复的消息 ID。', input: 'text' },
      { key: 'text', label: '回复文本', type: 'string', required: true, description: '回复内容。', input: 'text' },
    ],
    template: (connectionID) => ({ target: targetTemplate(connectionID), reply_to: '', text: 'reply from plugin api debug' }),
    exampleResponse: { accepted: true, method: 'messenger.reply_text', result: { sent: true }, message: '接口调用成功' },
  },
  {
    method: 'messenger.send_segments',
    category: '消息接口',
    label: '发送消息段',
    summary: '发送完整的 OneBot 消息段数组。',
    hint: '适合图片、文件、@ 提及和混合消息。',
    params: [
      { key: 'target', label: '消息目标', type: 'object', required: true, description: '消息目标对象。', input: 'json' },
      { key: 'segments', label: '消息段数组', type: 'list[dict]', required: true, description: 'OneBot 消息段数组。', input: 'json' },
    ],
    template: (connectionID) => ({ target: targetTemplate(connectionID), segments: [{ type: 'text', data: { text: 'hello from segments' } }] }),
    exampleResponse: { accepted: true, method: 'messenger.send_segments', result: { sent: true }, message: '接口调用成功' },
  },
  {
    method: 'bot.get_stranger_info',
    category: '用户接口',
    label: '获取陌生人信息',
    summary: '查询指定 QQ 用户的基础资料。',
    hint: '适合在 Python 插件里补全昵称和用户资料。',
    params: [
      { key: 'connection_id', label: '连接 ID', type: 'string', required: true, description: '本次调用使用的连接 ID。', input: 'text' },
      { key: 'user_id', label: '用户 QQ', type: 'string | number', required: true, description: '目标用户 QQ 号。', input: 'text' },
    ],
    template: (connectionID) => ({ connection_id: connectionID, user_id: '' }),
    exampleResponse: { accepted: true, method: 'bot.get_stranger_info', result: { user_id: '123456', nickname: 'Alice', sex: 'female', age: 18 }, message: '接口调用成功' },
  },
  {
    method: 'bot.get_group_info',
    category: '群接口',
    label: '获取群信息',
    summary: '查询指定群的基础资料和成员数量。',
    hint: '适合在群插件里确认群名称、人数和目标群是否正确。',
    params: [
      { key: 'connection_id', label: '连接 ID', type: 'string', required: true, description: '本次调用使用的连接 ID。', input: 'text' },
      { key: 'group_id', label: '群号', type: 'string | number', required: true, description: '目标群号。', input: 'text' },
    ],
    template: (connectionID) => ({ connection_id: connectionID, group_id: '' }),
    exampleResponse: { accepted: true, method: 'bot.get_group_info', result: { group_id: '123456', group_name: '测试群', member_count: 32, max_member_count: 200 }, message: '接口调用成功' },
  },
  {
    method: 'bot.get_group_member_list',
    category: '群接口',
    label: '获取群成员列表',
    summary: '拉取指定群的成员列表。',
    hint: '适合做群成员筛选、抽样或批量检查。',
    params: [
      { key: 'connection_id', label: '连接 ID', type: 'string', required: true, description: '本次调用使用的连接 ID。', input: 'text' },
      { key: 'group_id', label: '群号', type: 'string | number', required: true, description: '目标群号。', input: 'text' },
    ],
    template: (connectionID) => ({ connection_id: connectionID, group_id: '' }),
    exampleResponse: { accepted: true, method: 'bot.get_group_member_list', result: [{ group_id: '123456', user_id: '10001', nickname: 'Alice', role: 'member' }], message: '接口调用成功' },
  },
  {
    method: 'bot.get_group_member_info',
    category: '群接口',
    label: '获取群成员信息',
    summary: '查询指定群成员的详细资料。',
    hint: '适合查看角色、群名片、头衔和权限信息。',
    params: [
      { key: 'connection_id', label: '连接 ID', type: 'string', required: true, description: '本次调用使用的连接 ID。', input: 'text' },
      { key: 'group_id', label: '群号', type: 'string | number', required: true, description: '目标群号。', input: 'text' },
      { key: 'user_id', label: '用户 QQ', type: 'string | number', required: true, description: '目标用户 QQ 号。', input: 'text' },
    ],
    template: (connectionID) => ({ connection_id: connectionID, group_id: '', user_id: '' }),
    exampleResponse: { accepted: true, method: 'bot.get_group_member_info', result: { group_id: '123456', user_id: '10001', nickname: 'Alice', role: 'admin' }, message: '接口调用成功' },
  },
  {
    method: 'bot.get_message',
    category: '消息查询',
    label: '获取消息详情',
    summary: '按消息 ID 拉取消息详情。',
    hint: '适合做回复链分析和消息内容排查。',
    params: [
      { key: 'connection_id', label: '连接 ID', type: 'string', required: true, description: '本次调用使用的连接 ID。', input: 'text' },
      { key: 'message_id', label: '消息 ID', type: 'string | number', required: true, description: '目标消息 ID。', input: 'text' },
    ],
    template: (connectionID) => ({ connection_id: connectionID, message_id: '' }),
    exampleResponse: { accepted: true, method: 'bot.get_message', result: { message_id: '998877', message_type: 'group', raw_message: 'hello' }, message: '接口调用成功' },
  },
  {
    method: 'bot.get_forward_message',
    category: '消息查询',
    label: '获取合并转发',
    summary: '按 forward_id 拉取合并转发消息节点。',
    hint: 'forward_id 通常来自 [CQ:forward,id=...] 消息段。',
    params: [
      { key: 'connection_id', label: '连接 ID', type: 'string', required: true, description: '本次调用使用的连接 ID。', input: 'text' },
      { key: 'forward_id', label: '合并转发 ID', type: 'string', required: true, description: '目标合并转发消息 ID。', input: 'text' },
    ],
    template: (connectionID) => ({ connection_id: connectionID, forward_id: '' }),
    exampleResponse: { accepted: true, method: 'bot.get_forward_message', result: { id: 'forward-1', nodes: [{ user_id: '10001', nickname: 'Alice', content: [{ type: 'text', data: { text: 'hello' } }] }] }, message: '接口调用成功' },
  },
  {
    method: 'bot.delete_message',
    category: '消息查询',
    label: '删除消息',
    summary: '按消息 ID 删除或撤回一条消息。',
    hint: '执行前先确认目标消息 ID 是否正确。',
    params: [
      { key: 'connection_id', label: '连接 ID', type: 'string', required: true, description: '本次调用使用的连接 ID。', input: 'text' },
      { key: 'message_id', label: '消息 ID', type: 'string | number', required: true, description: '目标消息 ID。', input: 'text' },
    ],
    template: (connectionID) => ({ connection_id: connectionID, message_id: '' }),
    exampleResponse: { accepted: true, method: 'bot.delete_message', result: { deleted: true }, message: '接口调用成功' },
  },
  {
    method: 'bot.resolve_media',
    category: '媒体接口',
    label: '解析媒体引用',
    summary: '把图片、文件、视频消息段里的引用解析成宿主媒体信息。',
    hint: '适合在 Python 插件里下载原始媒体文件。',
    params: [
      { key: 'connection_id', label: '连接 ID', type: 'string', required: true, description: '本次调用使用的连接 ID。', input: 'text' },
      { key: 'segment_type', label: '消息段类型', type: 'string', required: true, description: '消息段类型，例如 image / video / file。', input: 'text' },
      { key: 'file', label: '文件引用', type: 'string', required: true, description: '消息段中的文件引用值。', input: 'text' },
    ],
    template: (connectionID) => ({ connection_id: connectionID, segment_type: 'image', file: '' }),
    exampleResponse: { accepted: true, method: 'bot.resolve_media', result: { url: 'https://example.com/media/demo.jpg', file_name: 'demo.jpg', file_size: 2048 }, message: '接口调用成功' },
  },
  {
    method: 'bot.get_login_info',
    category: '连接接口',
    label: '获取登录信息',
    summary: '查询当前连接对应账号的登录信息。',
    hint: '适合在多连接环境里核对账号身份。',
    params: [{ key: 'connection_id', label: '连接 ID', type: 'string', required: true, description: '本次调用使用的连接 ID。', input: 'text' }],
    template: (connectionID) => ({ connection_id: connectionID }),
    exampleResponse: { accepted: true, method: 'bot.get_login_info', result: { user_id: '123456789', nickname: 'Bot' }, message: '接口调用成功' },
  },
  {
    method: 'bot.get_status',
    category: '连接接口',
    label: '获取连接状态',
    summary: '读取指定连接的在线状态和统计信息。',
    hint: '适合在发消息前做预检查。',
    params: [{ key: 'connection_id', label: '连接 ID', type: 'string', required: true, description: '本次调用使用的连接 ID。', input: 'text' }],
    template: (connectionID) => ({ connection_id: connectionID }),
    exampleResponse: { accepted: true, method: 'bot.get_status', result: { online: true, good: true }, message: '接口调用成功' },
  },
  {
    method: 'bot.send_group_forward',
    category: '群接口',
    label: '发送群合并转发',
    summary: '向指定群发送一条合并转发消息。',
    hint: '适合把多段插件结果折叠成一条群消息。',
    params: [
      { key: 'connection_id', label: '连接 ID', type: 'string', required: true, description: '本次调用使用的连接 ID。', input: 'text' },
      { key: 'group_id', label: '群号', type: 'string | number', required: true, description: '目标群号。', input: 'text' },
      { key: 'nodes', label: '转发节点', type: 'list[dict]', required: true, description: '合并转发节点数组。', input: 'json' },
      { key: 'options', label: '附加选项', type: 'object', required: false, description: '额外的转发选项。', input: 'json' },
    ],
    template: (connectionID) => ({ connection_id: connectionID, group_id: '', nodes: [{ user_id: 'bot', nickname: 'Plugin Debug', content: [{ type: 'text', data: { text: 'hello from plugin api debug' } }] }], options: { source: 'Plugin Debug' } }),
    exampleResponse: { accepted: true, method: 'bot.send_group_forward', result: { sent: true }, message: '接口调用成功' },
  },
]

export function toPythonLiteral(value: unknown, depth = 0): string {
  const pad = '    '.repeat(depth)
  const nextPad = '    '.repeat(depth + 1)
  if (value == null) return 'None'
  if (typeof value === 'string') return JSON.stringify(value)
  if (typeof value === 'number') return String(value)
  if (typeof value === 'boolean') return value ? 'True' : 'False'
  if (Array.isArray(value)) {
    if (!value.length) return '[]'
    return '[\n' + value.map((item) => nextPad + toPythonLiteral(item, depth + 1)).join(',\n') + '\n' + pad + ']'
  }
  if (typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>)
    if (!entries.length) return '{}'
    return '{\n' + entries.map(([key, item]) => nextPad + JSON.stringify(key) + ': ' + toPythonLiteral(item, depth + 1)).join(',\n') + '\n' + pad + '}'
  }
  return JSON.stringify(value)
}

export function buildPythonSample(definition: DebugMethodDefinition, payload: Record<string, unknown>): string {
  const method = definition.method
  if (method === 'messenger.send_text') {
    return ['target = ' + toPythonLiteral(payload.target), '', 'env.messenger.send_text(', '    target=target,', '    text=' + toPythonLiteral(payload.text || '', 1) + ',', ')'].join('\n')
  }
  if (method === 'messenger.reply_text') {
    return ['target = ' + toPythonLiteral(payload.target), '', 'env.messenger.reply_text(', '    target=target,', '    reply_to=' + toPythonLiteral(payload.reply_to || '', 1) + ',', '    text=' + toPythonLiteral(payload.text || '', 1) + ',', ')'].join('\n')
  }
  if (method === 'messenger.send_segments') {
    return ['target = ' + toPythonLiteral(payload.target), 'segments = ' + toPythonLiteral(payload.segments || []), '', 'env.messenger.send_segments(', '    target=target,', '    segments=segments,', ')'].join('\n')
  }
  if (method === 'bot.delete_message') {
    return ['env.bot_api.delete_message(', '    connection_id=' + toPythonLiteral(payload.connection_id || '', 1) + ',', '    message_id=' + toPythonLiteral(payload.message_id || '', 1) + ',', ')'].join('\n')
  }
  if (method === 'bot.send_group_forward') {
    return ['from gobot_runtime import ForwardOptions', '', 'nodes = ' + toPythonLiteral(payload.nodes || []), '', 'env.bot_api.send_group_forward(', '    connection_id=' + toPythonLiteral(payload.connection_id || '', 1) + ',', '    group_id=' + toPythonLiteral(payload.group_id || '', 1) + ',', '    nodes=nodes,', '    options=ForwardOptions(source=' + toPythonLiteral((payload.options as Record<string, unknown> | undefined)?.source || 'Plugin Debug', 1) + '),', ')'].join('\n')
  }
  const functionName = method.replace(/^bot\./, '')
  const args = Object.entries(payload).map(([key, value]) => `    ${key}=${toPythonLiteral(value, 1)},`).join('\n')
  return ['import json', '', `result = env.bot_api.${functionName}(`, args, ')', 'env.logger.info(json.dumps(result, ensure_ascii=False))'].join('\n')
}
