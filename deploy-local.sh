cd frontend
npm run build
cd ..
sudo rm -rf /opt/1panel/www/sites/loopback.altasci.com/index/*
sudo cp -r frontend/dist/* /opt/1panel/www/sites/loopback.altasci.com/index/
