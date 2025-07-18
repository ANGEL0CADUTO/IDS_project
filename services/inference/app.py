from flask import Flask, request, jsonify
import joblib
import numpy as np

app = Flask(__name__)

# Carica il modello all'avvio
model = joblib.load('isolation_forest_model.joblib')
print("Modello di inferenza caricato.")

@app.route('/predict', methods=['POST'])
def predict():
    data = request.get_json()
    if not data or 'features' not in data:
        return jsonify({"error": "Dati invalidi"}), 400

    features = np.array(data['features']).reshape(1, -1)

    # Esegui la predizione
    prediction = model.predict(features)

    # Il risultato è un array numpy, prendiamo il primo elemento
    result = int(prediction[0])

    return jsonify({"prediction": result})

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5000)