#!/bin/bash

# Configuration
TOTAL_URLS=1000000        # Total number of URLs to create
BATCH_SIZE=100           # Number of URLs per batch
PARALLEL_JOBS=10         # Number of parallel jobs
API_URL="http://localhost:8080/api/v1/shorten"

# ANSI color codes
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to generate and send a single URL
generate_url() {
    local id=$1
    local random_str=$(cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n 1)
    local data="{\"url\":\"https://example.com/${random_str}\",\"title\":\"Test URL ${id}\"}"
    
    curl -s -X POST \
        -H "Content-Type: application/json" \
        -d "$data" \
        "$API_URL" > /dev/null
}

# Function to process a batch of URLs
process_batch() {
    local start=$1
    local end=$2
    
    for ((i=start; i<end; i++)); do
        generate_url $i &
        
        # Limit concurrent requests within each batch
        if (( $(jobs -r | wc -l) >= 20 )); then
            wait -n
        fi
    done
    wait
}

# Main execution
echo -e "${GREEN}Starting load test...${NC}"
echo -e "${BLUE}Configuration:${NC}"
echo "Total URLs: $TOTAL_URLS"
echo "Batch Size: $BATCH_SIZE"
echo "Parallel Jobs: $PARALLEL_JOBS"
echo "API URL: $API_URL"
echo "----------------------------------------"
start_time=$(date +%s)

# Process URLs in parallel batches
for ((i=0; i<TOTAL_URLS; i+=BATCH_SIZE*PARALLEL_JOBS)); do
    for ((j=0; j<PARALLEL_JOBS; j++)); do
        start=$((i + j*BATCH_SIZE))
        end=$((start + BATCH_SIZE))
        
        if ((end > TOTAL_URLS)); then
            end=$TOTAL_URLS
        fi
        
        process_batch $start $end &
    done
    wait
    
    # Progress update every 10000 URLs
    if ((i % 10000 == 0)); then
        current=$(date +%s)
        elapsed=$((current - start_time))
        rate=$(( $i / ($elapsed + 1) ))
        percent_complete=$(( ($i * 100) / $TOTAL_URLS ))
        echo -e "${GREEN}Processed $i URLs${NC} - ${BLUE}$percent_complete%${NC} complete (Rate: $rate URLs/sec)"
    fi
done

end_time=$(date +%s)
total_time=$((end_time - start_time))
rate=$(( $TOTAL_URLS / ($total_time + 1) ))

echo -e "\n${GREEN}Load test completed!${NC}"
echo -e "${BLUE}Final Statistics:${NC}"
echo "----------------------------------------"
echo "Total URLs processed: $TOTAL_URLS"
echo "Total time: $total_time seconds"
echo "Average rate: $rate URLs/sec"
echo "----------------------------------------"
