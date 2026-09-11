#!/usr/bin/env python3
"""
Embedding service for puff CLI - supports Python-only embedding models
"""

from flask import Flask, request, jsonify
from sentence_transformers import SentenceTransformer
import torch
import logging
import os
from typing import Dict, List
import sys

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

app = Flask(__name__)

# Model cache to avoid reloading
model_cache: Dict[str, SentenceTransformer] = {}

# Set cache directory
CACHE_DIR = os.environ.get('TRANSFORMERS_CACHE', '/app/model_cache')


def load_model(model_name: str) -> SentenceTransformer:
    """Load a model, using cache if available"""
    if model_name in model_cache:
        logger.info(f"Using cached model: {model_name}")
        return model_cache[model_name]

    logger.info(f"Loading model: {model_name}")
    try:
        model = SentenceTransformer(model_name, cache_folder=CACHE_DIR)
        model_cache[model_name] = model
        logger.info(f"Model loaded successfully: {model_name}")
        return model
    except Exception as e:
        logger.error(f"Failed to load model {model_name}: {str(e)}")
        raise


@app.route('/health', methods=['GET'])
def health():
    """Health check endpoint"""
    return jsonify({
        'status': 'healthy',
        'models_loaded': list(model_cache.keys()),
        'cache_dir': CACHE_DIR
    })


@app.route('/models', methods=['GET'])
def models():
    """List loaded models"""
    return jsonify({
        'loaded_models': list(model_cache.keys()),
        'cache_dir': CACHE_DIR
    })


@app.route('/embed', methods=['POST'])
def embed():
    """Generate embeddings for text"""
    try:
        data = request.get_json()

        if not data:
            return jsonify({'error': 'No JSON data provided'}), 400

        text = data.get('text')
        model_name = data.get('model')

        if not text:
            return jsonify({'error': 'Missing required field: text'}), 400

        if not model_name:
            return jsonify({'error': 'Missing required field: model'}), 400

        logger.info(f"Embedding request for model: {model_name}")

        # Load model
        model = load_model(model_name)

        # Generate embedding
        embedding = model.encode(text, convert_to_numpy=True)

        # Convert to list for JSON serialization
        embedding_list = embedding.tolist()

        logger.info(f"Generated embedding with {len(embedding_list)} dimensions")

        return jsonify({
            'embedding': embedding_list,
            'model': model_name,
            'dimensions': len(embedding_list)
        })

    except Exception as e:
        logger.error(f"Error generating embedding: {str(e)}", exc_info=True)
        return jsonify({
            'error': str(e),
            'type': type(e).__name__
        }), 500


@app.route('/clear-cache', methods=['POST'])
def clear_cache():
    """Clear the model cache"""
    global model_cache
    models_cleared = list(model_cache.keys())
    model_cache.clear()

    # Force garbage collection
    import gc
    gc.collect()
    if torch.cuda.is_available():
        torch.cuda.empty_cache()

    logger.info(f"Cleared {len(models_cleared)} models from cache")
    return jsonify({
        'status': 'success',
        'models_cleared': models_cleared
    })


if __name__ == '__main__':
    port = int(os.environ.get('PORT', 5050))
    logger.info(f"Starting embedding service on port {port}")
    logger.info(f"Model cache directory: {CACHE_DIR}")

    # Run the Flask app
    app.run(host='0.0.0.0', port=port, debug=False)
