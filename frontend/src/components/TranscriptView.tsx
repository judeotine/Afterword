'use client';

import { Transcript } from '@/types';
import { useEffect, useRef, useState } from 'react';
import { ConfidenceIndicator } from './ConfidenceIndicator';
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip';
import { RecordingStatusBar } from './RecordingStatusBar';
import { motion, AnimatePresence } from 'framer-motion';

interface TranscriptViewProps {
  transcripts: Transcript[];
  isRecording?: boolean;
  isPaused?: boolean;
  isProcessing?: boolean;
  isStopping?: boolean;
  enableStreaming?: boolean;
}

interface SpeechDetectedEvent {
  message: string;
}

function formatRecordingTime(seconds: number | undefined): string {
  if (seconds === undefined) return '[--:--]';

  const totalSeconds = Math.floor(seconds);
  const minutes = Math.floor(totalSeconds / 60);
  const secs = totalSeconds % 60;

  return `[${minutes.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}]`;
}

function cleanRepetitions(text: string): string {
  if (!text || text.trim().length === 0) return text;

  const words = text.split(/\s+/);
  const cleanedWords: string[] = [];

  let i = 0;
  while (i < words.length) {
    const currentWord = words[i];
    const currentWordLower = currentWord.toLowerCase();

    let repeatCount = 1;
    while (
      i + repeatCount < words.length &&
      words[i + repeatCount].toLowerCase() === currentWordLower
    ) {
      repeatCount++;
    }

    if (currentWord.length <= 2) {
      if (repeatCount >= 2) {
        cleanedWords.push(currentWord);
        i += repeatCount;
      } else {
        cleanedWords.push(currentWord);
        i += 1;
      }
    } else {
      if (repeatCount >= 3) {
        cleanedWords.push(currentWord);
        i += repeatCount;
      } else {
        cleanedWords.push(currentWord);
        i += 1;
      }
    }
  }

  return cleanedWords.join(' ');
}

function cleanStopWords(text: string): string {
  let cleanedText = cleanRepetitions(text);

  const stopWords = [
    'uh', 'um', 'er', 'ah', 'hmm', 'hm', 'eh', 'oh',
    // 'like', 'you know', 'i mean', 'sort of', 'kind of',
    // 'basically', 'actually', 'literally', 'right',
    // 'thank you', 'thanks'
  ];

  stopWords.forEach(word => {
    const pattern = new RegExp(`\\b${word}\\b[,\\s]*`, 'gi');
    cleanedText = cleanedText.replace(pattern, ' ');
  });

  cleanedText = cleanedText.replace(/\s+/g, ' ').trim();

  return cleanedText;
}

export const TranscriptView: React.FC<TranscriptViewProps> = ({ transcripts, isRecording = false, isPaused = false, isProcessing = false, isStopping = false, enableStreaming = false }) => {
  const [speechDetected, setSpeechDetected] = useState(false);

  console.log('TranscriptView render:', {
    isRecording,
    isPaused,
    isProcessing,
    isStopping,
    transcriptCount: transcripts.length,
    shouldShowListening: !isStopping && isRecording && !isPaused && !isProcessing && transcripts.length > 0
  });

  const [streamingTranscript, setStreamingTranscript] = useState<{
    id: string;
    visibleText: string;
    fullText: string;
  } | null>(null);
  const streamingIntervalRef = useRef<NodeJS.Timeout | null>(null);
  const lastStreamedIdRef = useRef<string | null>(null);

  const [showConfidence, setShowConfidence] = useState<boolean>(() => {
    if (typeof window !== 'undefined') {
      const saved = localStorage.getItem('showConfidenceIndicator');
      return saved !== null ? saved === 'true' : true;
    }
    return true;
  });

  useEffect(() => {
    const handleConfidenceChange = (e: Event) => {
      const customEvent = e as CustomEvent<boolean>;
      setShowConfidence(customEvent.detail);
    };

    window.addEventListener('confidenceIndicatorChanged', handleConfidenceChange);
    return () => window.removeEventListener('confidenceIndicatorChanged', handleConfidenceChange);
  }, []);

  useEffect(() => {
    let unsubscribe: (() => void) | undefined;

    const setupListener = async () => {
      const { listen } = await import('@tauri-apps/api/event');
      unsubscribe = await listen<SpeechDetectedEvent>('speech-detected', () => {
        setSpeechDetected(true);
      });
    };

    if (isRecording) {
      setupListener();
    } else {
      setSpeechDetected(false);
    }

    return () => {
      if (unsubscribe) {
        unsubscribe();
      }
    };
  }, [isRecording]);

  useEffect(() => {
    if (!enableStreaming || !isRecording) {
      if (streamingIntervalRef.current) {
        clearInterval(streamingIntervalRef.current);
        streamingIntervalRef.current = null;
      }
      setStreamingTranscript(null);
      lastStreamedIdRef.current = null;
      return;
    }

    const latestTranscript = transcripts
      .slice(-1)[0];

    if (!latestTranscript) return;

    if (lastStreamedIdRef.current !== latestTranscript.id) {
      if (streamingIntervalRef.current) {
        clearInterval(streamingIntervalRef.current);
        streamingIntervalRef.current = null;
      }

      lastStreamedIdRef.current = latestTranscript.id;

      const fullText = latestTranscript.text;

      const TOTAL_DURATION_MS = 800;
      const INTERVAL_MS = 15;
      const totalTicks = TOTAL_DURATION_MS / INTERVAL_MS;
      const charsPerTick = Math.max(2, Math.ceil(fullText.length / totalTicks));
      const INITIAL_CHARS = Math.min(5, fullText.length);
      let charIndex = INITIAL_CHARS;

      setStreamingTranscript({
        id: latestTranscript.id,
        visibleText: fullText.substring(0, INITIAL_CHARS),
        fullText: fullText
      });

      streamingIntervalRef.current = setInterval(() => {
        charIndex += charsPerTick;

        if (charIndex >= fullText.length) {
          clearInterval(streamingIntervalRef.current!);
          streamingIntervalRef.current = null;
          setStreamingTranscript(null);
        } else {
          setStreamingTranscript(prev => {
            if (!prev) return null;
            return {
              ...prev,
              visibleText: fullText.substring(0, charIndex)
            };
          });
        }
      }, INTERVAL_MS);
    }
  }, [transcripts, enableStreaming, isRecording]);

  useEffect(() => {
    return () => {
      if (streamingIntervalRef.current) {
        clearInterval(streamingIntervalRef.current);
        streamingIntervalRef.current = null;
      }
      lastStreamedIdRef.current = null;
    };
  }, []);

  return (
    <div className="px-4 py-2">
      {/* Recording Status Bar - Sticky at top, always visible when recording */}
      <AnimatePresence>
        {isRecording && (
          <div className="sticky top-4 z-10 bg-white pb-2">
            <RecordingStatusBar isPaused={isPaused} />
          </div>
        )}
      </AnimatePresence>

      {transcripts?.map((transcript, index) => {
        const isStreaming = streamingTranscript?.id === transcript.id;
        const textToShow = isStreaming ? streamingTranscript.visibleText : transcript.text;
        const filteredText = cleanStopWords(textToShow);
        const originalWasEmpty = transcript.text.trim() === '';
        const displayText = originalWasEmpty && !isStreaming ? '[Silence]' : filteredText;

        const sizerText = cleanStopWords(isStreaming ? streamingTranscript.fullText : transcript.text)
          || (originalWasEmpty && !isStreaming ? '[Silence]' : '');

        return (
          <motion.div
            key={transcript.id ? `${transcript.id}-${index}` : `transcript-${index}`}
            initial={{ opacity: 0, y: 5 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.15 }}
            className="mb-3"
          >
            <div className="flex items-start gap-2">
              <Tooltip>
                <TooltipTrigger>
                  <span className="text-xs text-gray-400 mt-1 flex-shrink-0 min-w-[50px]">
                    {transcript.audio_start_time !== undefined
                      ? formatRecordingTime(transcript.audio_start_time)
                      : transcript.timestamp}
                  </span>
                </TooltipTrigger>
                <TooltipContent>
                  {transcript.duration !== undefined && (
                    <span className="text-xs text-gray-400">
                      {transcript.duration.toFixed(1)}s
                      {transcript.confidence !== undefined && (
                        <ConfidenceIndicator
                          confidence={transcript.confidence}
                          showIndicator={showConfidence}
                        />
                      )}
                    </span>
                  )}
                </TooltipContent>
              </Tooltip>
              <div className="flex-1">
                {isStreaming ? (
                  <div className="bg-gray-100 border border-gray-200 rounded-lg px-3 py-2">
                    <div className="relative">
                      <p className="text-base text-gray-800 leading-relaxed" style={{ visibility: 'hidden' }}>
                        {sizerText}
                      </p>
                      <p className="text-base text-gray-800 leading-relaxed absolute top-0 left-0">
                        {displayText}
                      </p>
                    </div>
                  </div>
                ) : (
                  <div className="relative">
                    <p className="text-base text-gray-800 leading-relaxed" style={{ visibility: 'hidden' }}>
                      {sizerText}
                    </p>
                    <p className="text-base text-gray-800 leading-relaxed absolute top-0 left-0">
                      {displayText}
                    </p>
                  </div>
                )}
              </div>
            </div>
          </motion.div>
        );
      })}

      {/* Show listening indicator when recording and has transcripts */}
      {!isStopping && isRecording && !isPaused && !isProcessing && transcripts.length > 0 && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          className="flex items-center gap-2 mt-4 text-gray-500"
        >
          <div className="w-2 h-2 bg-blue-500 rounded-full animate-pulse"></div>
          <span className="text-sm">Listening...</span>
        </motion.div>
      )}

      {/* Empty state when no transcripts */}
      {transcripts.length === 0 && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          className="text-center text-gray-500 mt-8"
        >
          {isRecording ? (
            <>
              <div className="flex items-center justify-center mb-3">
                <div className={`w-3 h-3 rounded-full ${isPaused ? 'bg-orange-500' : 'bg-blue-500 animate-pulse'}`}></div>
              </div>
              <p className="text-sm text-gray-600">
                {isPaused ? 'Recording paused' : 'Listening for speech...'}
              </p>
              <p className="text-xs mt-1 text-gray-400">
                {isPaused
                  ? 'Click resume to continue recording'
                  : 'Speak to see live transcription'}
              </p>
            </>
          ) : (
            <>
              <p className="text-lg font-semibold">Welcome to Afterword!</p>
              <p className="text-xs mt-1">Start recording to see live transcription</p>
            </>
          )}
        </motion.div>
      )}
    </div>
  );
};
