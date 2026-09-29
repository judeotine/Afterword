import requests
import time
import argparse
import json
import sys
import uuid
import logging

DEFAULT_BASE_URL = "http://localhost:5167"
DEFAULT_MODEL_PROVIDER = "openai"
DEFAULT_MODEL_NAME = "gpt-4o-2024-11-20"
DEFAULT_CHUNK_SIZE = 40000
DEFAULT_OVERLAP = 1000
DEFAULT_POLL_INTERVAL_SECONDS = 5
DEFAULT_MAX_POLL_ATTEMPTS = 24

logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)

def process_transcript(base_url, transcript_text, provider, model_name, chunk_size, overlap, meeting_id):
    """Sends the transcript to the processing endpoint."""
    url = f"{base_url}/process-transcript"
    payload = {
        "text": transcript_text,
        "model": provider,
        "model_name": model_name,
        "meeting_id": meeting_id,
        "chunk_size": chunk_size,
        "overlap": overlap
    }
    headers = {'Content-Type': 'application/json'}
    logger.info(f"Sending POST request to {url} with model '{provider}/{model_name}' and meeting_id '{meeting_id}'...")
    logger.debug(f"Payload: {json.dumps(payload, indent=2)}")

    try:
        response = requests.post(url, headers=headers, json=payload, timeout=30)
        logger.info(f"POST Response Status Code: {response.status_code}")
        response.raise_for_status()

        response_data = response.json()
        if "process_id" in response_data:

            returned_process_id = response_data['process_id']
            logger.info(f"Successfully initiated processing. Process ID received: {returned_process_id}")

            if returned_process_id != meeting_id:
                 logger.warning(f"Returned process_id '{returned_process_id}' differs from generated meeting_id '{meeting_id}'. Using returned ID for polling.")
            return returned_process_id
        else:
            logger.error(f"'process_id' not found in response: {response_data}")
            return None

    except requests.exceptions.Timeout:
        logger.error(f"Error: Request to {url} timed out.")
        return None
    except requests.exceptions.RequestException as e:
        logger.error(f"Error during transcript processing request: {e}")
        if e.response is not None:
             logger.error(f"Response status: {e.response.status_code}, Response text: {e.response.text}")
        return None
    except json.JSONDecodeError:
        logger.error(f"Could not decode JSON response from {url}. Response text: {response.text}")
        return None

def poll_summary_status(base_url, meeting_id_for_polling, interval, max_attempts):
    """Polls the summary status endpoint until completion or error, using meeting_id."""

    url = f"{base_url}/get-summary/{meeting_id_for_polling}"
    logger.info(f"Polling status endpoint: {url} (every {interval}s) for meeting_id '{meeting_id_for_polling}'")

    for attempt in range(max_attempts):
        logger.info(f"Polling attempt {attempt + 1}/{max_attempts}...")
        try:
            response = requests.get(url, timeout=20)
            logger.info(f"GET Response Status Code: {response.status_code}")

            if response.status_code == 202:
                status_data = response.json()
                status = status_data.get("status", "processing").lower()
                logger.info(f"  Status: {status} (via 202 Accepted)")
                time.sleep(interval)
                continue

            response.raise_for_status()

            status_data = response.json()
            status = status_data.get("status", "unknown").lower()
            error_message = status_data.get("error")
            summary_data = status_data.get("data")
            meeting_name = status_data.get("meetingName")

            logger.info(f"  Status: {status}")
            if meeting_name:
                logger.info(f"  Meeting Name: {meeting_name}")

            if status == "completed":
                logger.info("Processing completed successfully!")
                if summary_data:
                     return summary_data
                else:
                     logger.error("Status is 'completed' but 'data' field is missing or empty in the response.")
                     return None
            elif status == "error" or status == "failed":
                logger.error(f"Error reported by backend: {error_message or 'Unknown error'}")
                return None
            elif status in ["processing", "pending", "started"]:

                time.sleep(interval)
            else:
                logger.warning(f"Received unknown status '{status}'. Response: {status_data}. Continuing to poll.")
                time.sleep(interval)

        except requests.exceptions.Timeout:
            logger.warning(f"Polling request timed out. Retrying...")
            time.sleep(interval)
        except requests.exceptions.RequestException as e:
            logger.error(f"Error during polling request: {e}. Stopping polling.")
            if e.response is not None:
                logger.error(f"Response status: {e.response.status_code}, Response text: {e.response.text}")

                if e.response.status_code == 404:
                    logger.error(f"Meeting ID '{meeting_id_for_polling}' not found on server. Ensure processing started correctly.")
            return None
        except json.JSONDecodeError:
            logger.error(f"Could not decode JSON response from {url}. Response text: {response.text}")
            logger.error("Stopping polling.")
            return None

    logger.error(f"Reached maximum polling attempts ({max_attempts}) without completion.")
    return None

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Test the transcript summarization API workflow.")
    parser.add_argument("transcript_file", help="Path to the .txt transcript file.")
    parser.add_argument("--base-url", default=DEFAULT_BASE_URL, help=f"Base URL of the API (default: {DEFAULT_BASE_URL})")
    parser.add_argument("--provider", default=DEFAULT_MODEL_PROVIDER, help=f"Model provider (default: {DEFAULT_MODEL_PROVIDER})")
    parser.add_argument("--model-name", default=DEFAULT_MODEL_NAME, help=f"Specific model name (default: {DEFAULT_MODEL_NAME})")
    parser.add_argument("--interval", type=int, default=DEFAULT_POLL_INTERVAL_SECONDS, help=f"Polling interval in seconds (default: {DEFAULT_POLL_INTERVAL_SECONDS})")
    parser.add_argument("--attempts", type=int, default=DEFAULT_MAX_POLL_ATTEMPTS, help=f"Maximum polling attempts (default: {DEFAULT_MAX_POLL_ATTEMPTS})")
    parser.add_argument("--chunk-size", type=int, default=DEFAULT_CHUNK_SIZE, help=f"Chunk size for processing (default: {DEFAULT_CHUNK_SIZE})")
    parser.add_argument("--overlap", type=int, default=DEFAULT_OVERLAP, help=f"Overlap size for processing (default: {DEFAULT_OVERLAP})")

    args = parser.parse_args()

    try:
        with open(args.transcript_file, 'r', encoding='utf-8') as f:
            transcript_content = f.read()
        logger.info(f"Successfully read transcript file: {args.transcript_file}")
        if not transcript_content.strip():
             logger.error("Transcript file is empty.")
             sys.exit(1)
    except FileNotFoundError:
        logger.error(f"Transcript file not found at '{args.transcript_file}'")
        sys.exit(1)
    except Exception as e:
        logger.error(f"Error reading transcript file: {e}")
        sys.exit(1)

    meeting_id = f"test-meeting-{uuid.uuid4()}"
    logger.info(f"Generated Meeting ID for this run: {meeting_id}")

    process_id_from_api = process_transcript(
        args.base_url,
        transcript_content,
        args.provider,
        args.model_name,
        args.chunk_size,
        args.overlap,
        meeting_id
    )

    if not process_id_from_api:
        logger.error("Failed to initiate transcript processing. Exiting.")
        sys.exit(1)

    summary_result = poll_summary_status(
        args.base_url,
        process_id_from_api,
        args.interval,
        args.attempts
    )

    if summary_result:
        logger.info("\\n--- Summary Received ---")

        print(json.dumps(summary_result, indent=2))
        logger.info("------------------------")
    else:
        logger.error("\\nFailed to retrieve summary.")
        sys.exit(1)

    logger.info("Script finished.")
