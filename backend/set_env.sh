#!/bin/bash

echo "Setting up environment variables..."
cp temp.env .env

update_api_key() {
    local key_name=$1
    local key_value=$2
    sed -i "" "s|$key_name=.*|$key_name=$key_value|g" .env
}

needs_update() {
    local value=$1
    [[ -z "$value" || "$value" == "api_key_here" || "$value" == "gapi_key_here" ]]
}

for key in ANTHROPIC_API_KEY GROQ_API_KEY OPENAI_API_KEY; do

    current_value="${!key}"

    if needs_update "$current_value"; then
        echo "$key is not set. Press Enter to skip or enter your API key:"
        read -p "Enter $key (or press Enter to skip): " new_value
        if [ -n "$new_value" ]; then
            update_api_key "$key" "$new_value"
        fi
    else
        update_api_key "$key" "$current_value"
    fi
done

echo "Final API Keys:"
grep -E "^(ANTHROPIC|GROQ|OPENAI)_API_KEY=" .env
echo "Environment setup complete!"
