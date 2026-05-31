import { Event, PromotionReason, ForgettingReason, LayerState } from '../types';
import { now } from '../utils/time';

/**
 * Log entry type
 */
export enum LogType {
  PROMOTION = 'promotion',
  FORGETTING = 'forgetting',
  REPLAY = 'replay',
  DECAY = 'decay',
  CONSOLIDATION = 'consolidation'
}

/**
 * Log entry
 */
export interface LogEntry {
  id: string;
  timestamp: number;
  type: LogType;
  eventId: string;
  fromLayer?: LayerState;
  toLayer?: LayerState;
  reason: string;
  score?: number;
  details: string;
}

/**
 * Statistics summary
 */
export interface StatsSummary {
  totalPromotions: number;
  totalForgetting: number;
  totalReplays: number;
  promotionsByReason: Map<PromotionReason, number>;
  forgettingByReason: Map<ForgettingReason, number>;
  avgPromotionScore: number;
  layer0ToLayer1: number;
  layer1ToLayer2: number;
}

/**
 * Explainability Logger
 * Records detailed reasons for all promotions, forgetting, and replays
 * Used for system tuning, auditing, and explainability
 */
export class ExplainabilityLogger {
  private logs: LogEntry[] = [];
  private maxLogSize: number = 10000; // Keep maximum 10000 log entries
  
  /**
   * Log promotion
   */
  logPromotion(
    event: Event,
    fromLayer: LayerState,
    toLayer: LayerState,
    reason: PromotionReason,
    score: number,
    details: string
  ): void {
    const entry: LogEntry = {
      id: `log_${now()}_${Math.random().toString(36).substr(2, 9)}`,
      timestamp: now(),
      type: LogType.PROMOTION,
      eventId: event.id,
      fromLayer,
      toLayer,
      reason,
      score,
      details: `Promoted from Layer ${fromLayer} to Layer ${toLayer}. ${details}`
    };
    
    this.addLog(entry);
  }
  
  /**
   * Log forgetting
   */
  logForgetting(
    eventId: string,
    layer: LayerState,
    reason: ForgettingReason,
    details: string
  ): void {
    const entry: LogEntry = {
      id: `log_${now()}_${Math.random().toString(36).substr(2, 9)}`,
      timestamp: now(),
      type: LogType.FORGETTING,
      eventId,
      fromLayer: layer,
      reason,
      details: `Forgotten from Layer ${layer}. Reason: ${reason}. ${details}`
    };
    
    this.addLog(entry);
  }
  
  /**
   * Log replay
   */
  logReplay(
    event: Event,
    scoreBoost: number,
    details: string
  ): void {
    const entry: LogEntry = {
      id: `log_${now()}_${Math.random().toString(36).substr(2, 9)}`,
      timestamp: now(),
      type: LogType.REPLAY,
      eventId: event.id,
      fromLayer: event.layerState,
      score: scoreBoost,
      reason: 'replay_consolidation',
      details: `Replayed in Layer ${event.layerState}. Score boost: ${scoreBoost.toFixed(3)}. ${details}`
    };
    
    this.addLog(entry);
  }
  
  /**
   * Log decay
   */
  logDecay(
    eventId: string,
    layer: LayerState,
    oldScore: number,
    newScore: number
  ): void {
    const entry: LogEntry = {
      id: `log_${now()}_${Math.random().toString(36).substr(2, 9)}`,
      timestamp: now(),
      type: LogType.DECAY,
      eventId,
      fromLayer: layer,
      reason: 'time_decay',
      score: newScore,
      details: `Decayed in Layer ${layer}. Score: ${oldScore.toFixed(3)} -> ${newScore.toFixed(3)}`
    };
    
    this.addLog(entry);
  }
  
  /**
   * Log consolidation
   */
  logConsolidation(
    schemaId: string,
    eventIds: string[],
    details: string
  ): void {
    const entry: LogEntry = {
      id: `log_${now()}_${Math.random().toString(36).substr(2, 9)}`,
      timestamp: now(),
      type: LogType.CONSOLIDATION,
      eventId: schemaId,
      toLayer: LayerState.LONG_TERM,
      reason: 'schema_consolidation',
      details: `Consolidated ${eventIds.length} events into schema. ${details}`
    };
    
    this.addLog(entry);
  }
  
  /**
   * Add log entry
   */
  private addLog(entry: LogEntry): void {
    this.logs.push(entry);
    
    // Maintain log size limit
    if (this.logs.length > this.maxLogSize) {
      this.logs = this.logs.slice(-this.maxLogSize);
    }
  }
  
  /**
   * Get logs for specific event
   */
  getEventLogs(eventId: string): LogEntry[] {
    return this.logs.filter(log => log.eventId === eventId);
  }
  
  /**
   * Get logs by type
   */
  getLogsByType(type: LogType): LogEntry[] {
    return this.logs.filter(log => log.type === type);
  }
  
  /**
   * Get logs by time range
   */
  getLogsByTimeRange(startTime: number, endTime: number): LogEntry[] {
    return this.logs.filter(log => 
      log.timestamp >= startTime && log.timestamp <= endTime
    );
  }
  
  /**
   * Get recent N logs
   */
  getRecentLogs(count: number = 100): LogEntry[] {
    return this.logs.slice(-count);
  }
  
  /**
   * Generate statistics summary
   */
  generateStatsSummary(): StatsSummary {
    const promotionLogs = this.logs.filter(log => log.type === LogType.PROMOTION);
    const forgettingLogs = this.logs.filter(log => log.type === LogType.FORGETTING);
    const replayLogs = this.logs.filter(log => log.type === LogType.REPLAY);
    
    const promotionsByReason = new Map<PromotionReason, number>();
    const forgettingByReason = new Map<ForgettingReason, number>();
    
    let totalScore = 0;
    let scoreCount = 0;
    let layer0ToLayer1 = 0;
    let layer1ToLayer2 = 0;
    
    for (const log of promotionLogs) {
      const reason = log.reason as PromotionReason;
      promotionsByReason.set(reason, (promotionsByReason.get(reason) || 0) + 1);
      
      if (log.score !== undefined) {
        totalScore += log.score;
        scoreCount++;
      }
      
      if (log.fromLayer === LayerState.SHORT_TERM && log.toLayer === LayerState.MID_TERM) {
        layer0ToLayer1++;
      } else if (log.fromLayer === LayerState.MID_TERM && log.toLayer === LayerState.LONG_TERM) {
        layer1ToLayer2++;
      }
    }
    
    for (const log of forgettingLogs) {
      const reason = log.reason as ForgettingReason;
      forgettingByReason.set(reason, (forgettingByReason.get(reason) || 0) + 1);
    }
    
    return {
      totalPromotions: promotionLogs.length,
      totalForgetting: forgettingLogs.length,
      totalReplays: replayLogs.length,
      promotionsByReason,
      forgettingByReason,
      avgPromotionScore: scoreCount > 0 ? totalScore / scoreCount : 0,
      layer0ToLayer1,
      layer1ToLayer2
    };
  }
  
  /**
   * Export logs as JSON
   */
  exportLogs(): string {
    return JSON.stringify(this.logs, null, 2);
  }
  
  /**
   * Clear logs
   */
  clearLogs(): void {
    this.logs = [];
  }
  
  /**
   * Get all logs
   */
  getAllLogs(): LogEntry[] {
    return [...this.logs];
  }
}
