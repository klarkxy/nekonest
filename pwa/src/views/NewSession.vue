<template>
  <div class="new-session-page">
    <div class="page-header">
      <n-button text @click="$router.back()">← 返回</n-button>
      <h1>如何开启会话</h1>
    </div>

    <div class="info-banner">
      手机端不能可靠地「从零创建」Claude Code / Codex 会话。
      正确路径是：在电脑上先打开 Agent，等会话出现在手机列表后再发指令。
    </div>

    <div class="steps neko-card">
      <h2>推荐流程</h2>
      <ol>
        <li>在 Windows 上启动 Claude Code 或 Codex，进入你要处理的项目目录。</li>
        <li>确认本机 <code>nekonest-daemon</code> 已连接服务器（在线）。</li>
        <li>回到手机 → 设备详情 / 会话列表，等待会话自动出现。</li>
        <li>点进会话，用手机发送 prompt、审批或中断。</li>
      </ol>
    </div>

    <div class="form-actions">
      <n-button type="primary" block size="large" @click="goSessions">
        🐾 查看会话列表
      </n-button>
      <n-button block style="margin-top: 10px" @click="$router.back()">
        返回设备
      </n-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'
import { NButton } from 'naive-ui'

const route = useRoute()
const router = useRouter()
const deviceId = route.params.deviceId as string

function goSessions() {
  router.push(`/device/${deviceId}/sessions`)
}
</script>

<style scoped>
.new-session-page {
  padding: 20px;
}
.info-banner {
  background: #e3f2fd;
  border-radius: 10px;
  padding: 12px;
  font-size: 13px;
  color: #1565c0;
  margin-bottom: 16px;
  line-height: 1.5;
}
.page-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 24px;
}
.page-header h1 {
  font-size: 20px;
  font-weight: 600;
}
.steps {
  padding: 16px;
  margin-bottom: 20px;
}
.steps h2 {
  font-size: 15px;
  margin: 0 0 10px;
}
.steps ol {
  margin: 0;
  padding-left: 20px;
  line-height: 1.7;
  font-size: 14px;
  color: #424242;
}
.steps code {
  font-size: 12px;
  background: #f5f5f5;
  padding: 1px 6px;
  border-radius: 4px;
}
.form-actions {
  margin-top: 8px;
}
</style>
