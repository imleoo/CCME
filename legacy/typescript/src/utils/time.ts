/**
 * 时间相关工具函数
 */

/**
 * 获取当前时间戳（毫秒）
 */
export function now(): number {
  return Date.now();
}

/**
 * 获取当前时间戳（秒）
 */
export function nowInSeconds(): number {
  return Math.floor(Date.now() / 1000);
}

/**
 * 计算时间差（秒）
 */
export function timeDiffInSeconds(timestampMs: number): number {
  return (Date.now() - timestampMs) / 1000;
}

/**
 * 检查是否过期
 */
export function isExpired(timestampMs: number, lifetimeSeconds: number): boolean {
  return timeDiffInSeconds(timestampMs) > lifetimeSeconds;
}

/**
 * 格式化时间差为可读字符串
 */
export function formatTimeDiff(seconds: number): string {
  if (seconds < 60) {
    return `${Math.floor(seconds)}秒`;
  } else if (seconds < 3600) {
    return `${Math.floor(seconds / 60)}分钟`;
  } else if (seconds < 86400) {
    return `${Math.floor(seconds / 3600)}小时`;
  } else {
    return `${Math.floor(seconds / 86400)}天`;
  }
}
