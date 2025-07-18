import joblib
import numpy as np
import skl2onnx
from skl2onnx import convert_sklearn
from skl2onnx.common.data_types import FloatTensorType

print("Inizio conversione del modello in formato ONNX...")

# 1. Caricare il modello addestrato
model_filename = '../services/inference/isolation_forest_model.joblib'
model = joblib.load(model_filename)
print(f"Modello '{model_filename}' caricato.")

# 2. Definire lo "schema" di input del modello
# ONNX ha bisogno di sapere che tipo di dati si aspetta in input.
# Il nostro modello si aspetta un vettore di float con 41 caratteristiche (le colonne del nostro dataset).
# Il primo argomento [None, 41] significa: "un numero imprecisato di campioni (None), ognuno con 41 feature".
initial_type = [('float_input', FloatTensorType([None, 41]))]

# 3. Eseguire la conversione
# Convertiamo il modello specificando il suo schema di input.
onnx_model = convert_sklearn(model, initial_types=initial_type, target_opset={'': 13, 'ai.onnx.ml': 3})
print("Modello convertito in formato ONNX.")

# 4. Salvare il modello ONNX su file
onnx_filename = '../models/isolation_forest_model.onnx'
with open(onnx_filename, "wb") as f:
    f.write(onnx_model.SerializeToString())

print(f"Modello salvato con successo come '{onnx_filename}'")

# 5. (Opzionale ma consigliato) Verificare la conversione
# Usiamo onnxruntime per assicurarci che il modello salvato sia valido e funzioni.
print("\nVerifica del modello ONNX...")
import onnxruntime as rt

# Creiamo un campione di dati finto (41 feature casuali)
# Deve essere un array di float32, che è il tipo che abbiamo definito.
dummy_input = np.random.rand(1, 41).astype(np.float32)

# Avviamo una sessione di inferenza con il modello ONNX
sess = rt.InferenceSession(onnx_filename)
input_name = sess.get_inputs()[0].name
label_name = sess.get_outputs()[0].name # Nome dell'output (di solito 'label')
score_name = sess.get_outputs()[1].name # Nome del punteggio di anomalia (di solito 'scores')

# Eseguiamo la predizione
pred_onx = sess.run([label_name, score_name], {input_name: dummy_input})

# pred_onx conterrà [array([predizione]), array([punteggio])]
# La predizione è -1 per anomalia, 1 per normale.
print(f"Verifica completata. Input di test eseguito.")
print(f"Predizione del modello ONNX sul campione finto: {pred_onx[0]}")
print(f"Punteggio di anomalia: {pred_onx[1]}")