import { Event, LayerState, PromotionReason } from '../types';
import { DEFAULT_CONFIG } from '../config/constants';
import { now, timeDiffInSeconds } from '../utils/time';
import { ShortTermBuffer } from '../storage/ShortTermBuffer';
import { MidTermStore } from '../storage/MidTermStore';
import { LongTermStore } from '../storage/LongTermStore';

/**
 * Promotion decision result
 */
export interface PromotionDecision {
  shouldPromote: boolean;
  reason: PromotionReason;
  score: number;
  details: string;
}

/**
 * Cascade Gates
 * Implements the "molecular timer cascade" of biological memory systems
 * Controls memory promotion between different layers
 */
export class CascadeGates {
  private config = DEFAULT_CONFIG;
  
  /**
   * Evaluate Layer 0 -> Layer 1 promotion
   * Corresponds to CAMTA1 -> TCF4 transition
   */
  shouldPromoteToLayer1(
    event: Event,
    shortTermBuffer: ShortTermBuffer
  ): PromotionDecision {
    // Check minimum wait time
    if (event.promotionEligibleAt && now() < event.promotionEligibleAt) {
      return {
        shouldPromote: false,
        reason: PromotionReason.HIGH_SALIENCE,
        score: 0,
        details: 'Minimum gate time not reached'
      };
    }
    
    // Calculate combined score
    const { alpha, beta, gamma } = this.config.SALIENCE_WEIGHTS;
    
    // 1. Raw salience
    const salience = event.scores.layer0Score || event.scores.rawSalience;
    
    // 2. Repetition factor
    const repetitionCount = event.metadata.repetitionCount || 0;
    const maxRepeat = 10; // Normalization upper limit
    const repeatFactor = Math.min(repetitionCount / maxRepeat, 1);
    
    // 3. Reward signal
    const rewardFactor = event.metadata.reward 
      ? Math.min(Math.abs(event.metadata.reward), 1) 
      : 0;
    
    // Combined score
    const combinedScore = alpha * salience + beta * repeatFactor + gamma * rewardFactor;
    
    // Determine if promotion should occur
    const threshold = this.config.PROMO_THRESH[0];
    const shouldPromote = combinedScore >= threshold;
    
    // Determine primary reason
    let reason = PromotionReason.HIGH_SALIENCE;
    if (repeatFactor > 0.5) {
      reason = PromotionReason.REPEATED_EXPOSURE;
    } else if (rewardFactor > 0.7) {
      reason = PromotionReason.TASK_REWARD;
    }
    
    return {
      shouldPromote,
      reason,
      score: combinedScore,
      details: `salience: ${salience.toFixed(3)}, repeat: ${repeatFactor.toFixed(3)}, ` +
               `reward: ${rewardFactor.toFixed(3)}, combined: ${combinedScore.toFixed(3)}, ` +
               `threshold: ${threshold.toFixed(3)}`
    };
  }
  
  /**
   * Evaluate Layer 1 -> Layer 2 promotion
   * Corresponds to TCF4 -> ASH1L transition (long-term consolidation)
   */
  shouldPromoteToLayer2(
    event: Event,
    midTermStore: MidTermStore
  ): PromotionDecision {
    // Check minimum wait time
    const ageSeconds = timeDiffInSeconds(event.createdAt);
    const minWaitTime = this.config.REPLAY.minWaitTime * 2; // Layer 1 requires longer wait
    
    if (ageSeconds < minWaitTime) {
      return {
        shouldPromote: false,
        reason: PromotionReason.STRUCTURAL_SUPPORT,
        score: 0,
        details: 'Minimum consolidation time not reached'
      };
    }
    
    // 1. Layer 1 score
    const layer1Score = event.scores.layer1Score || 
                       event.scores.layer0Score || 
                       event.scores.rawSalience;
    
    // 2. Structural support (graph centrality)
    const centrality = midTermStore.getCentrality(event.id);
    const structuralScore = centrality;
    
    // 3. Replay enhancement (statistics from history)
    const replayCount = event.history.filter(h => h.action === 'replayed').length;
    const replayScore = Math.min(replayCount / 5, 1); // At least 5 replays for max score
    
    // 4. Temporal stability (longer survival time should be retained)
    const ageDays = ageSeconds / 86400;
    const stabilityScore = Math.min(ageDays / 7, 1); // 7 days for max score
    
    // Combined score
    const combinedScore = 
      0.3 * layer1Score +
      0.3 * structuralScore +
      0.2 * replayScore +
      0.2 * stabilityScore;
    
    const threshold = this.config.PROMO_THRESH[1];
    const shouldPromote = combinedScore >= threshold;
    
    // Determine primary reason
    let reason = PromotionReason.STRUCTURAL_SUPPORT;
    if (replayScore > 0.7) {
      reason = PromotionReason.REPLAY_CONSOLIDATION;
    }
    
    return {
      shouldPromote,
      reason,
      score: combinedScore,
      details: `layer1: ${layer1Score.toFixed(3)}, structural: ${structuralScore.toFixed(3)}, ` +
               `replay: ${replayScore.toFixed(3)}, stability: ${stabilityScore.toFixed(3)}, ` +
               `combined: ${combinedScore.toFixed(3)}, threshold: ${threshold.toFixed(3)}`
    };
  }
  
  /**
   * Execute promotion
   */
  promoteEvent(
    event: Event,
    fromLayer: LayerState,
    toLayer: LayerState,
    reason: PromotionReason,
    score: number
  ): void {
    event.layerState = toLayer;
    
    // Update score
    switch (toLayer) {
      case LayerState.MID_TERM:
        event.scores.layer1Score = score;
        break;
      case LayerState.LONG_TERM:
        event.scores.layer2Score = score;
        break;
    }
    
    // Record history
    event.history.push({
      action: 'promoted',
      ts: now(),
      toLayer,
      reason: reason,
      score
    });
    
    // Update promotion eligibility time
    event.promotionEligibleAt = now() + this.config.REPLAY.minWaitTime * 1000;
  }
  
  /**
   * Batch evaluate promotion candidates
   */
  evaluateCandidates(
    events: Event[],
    evaluator: (event: Event) => PromotionDecision
  ): Array<{ event: Event; decision: PromotionDecision }> {
    return events
      .map(event => ({
        event,
        decision: evaluator(event)
      }))
      .filter(({ decision }) => decision.shouldPromote)
      .sort((a, b) => b.decision.score - a.decision.score);
  }
}
